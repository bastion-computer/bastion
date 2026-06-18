//go:build !darwin

// Package template manages environment templates.
package template

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/schema"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/secret"
)

// Template contains an environment template and its JSON configuration.
type Template struct {
	ID        string          `json:"id"`
	Key       *string         `json:"key,omitempty"`
	Config    json.RawMessage `json:"config"`
	CreatedAt string          `json:"createdAt"`
}

// Metadata describes a template without its full configuration payload.
type Metadata struct {
	ID        string  `json:"id"`
	Key       *string `json:"key,omitempty"`
	CreatedAt string  `json:"createdAt"`
}

// CreateRequest contains the fields needed to create a template.
type CreateRequest struct {
	Key    *string         `json:"key,omitempty"`
	Config json.RawMessage `json:"config"`
	Logs   io.Writer       `json:"-"`
}

// ImportRequest contains the fields needed to import a prepared template archive.
type ImportRequest struct {
	Key         *string   `json:"key,omitempty"`
	Archive     io.Reader `json:"-"`
	ArchiveSize int64     `json:"-"`
}

// ArchiveContentType is the media type used for template import/export streams.
const ArchiveContentType = ch.TemplateArchiveContentType

// Stream event types used by POST /v1/templates.
const (
	StreamEventLog    = "log"
	StreamEventResult = "result"
	StreamEventError  = "error"
)

// CreateStreamEvent is one line in a streamed template creation response.
type CreateStreamEvent struct {
	Type     string    `json:"type"`
	Log      string    `json:"log,omitempty"`
	Template *Metadata `json:"template,omitempty"`
	Error    string    `json:"error,omitempty"`
	Status   int       `json:"status,omitempty"`
}

// Orchestrator prepares and removes template VM artifacts.
type Orchestrator interface {
	PrepareTemplate(context.Context, ch.PrepareTemplateRequest) (ch.PreparedTemplate, error)
	RemoveTemplate(context.Context, string) (ch.PreparedTemplate, error)
	ExportTemplate(context.Context, ch.ExportTemplateRequest) error
	ImportTemplate(context.Context, ch.ImportTemplateRequest) (ch.ImportedTemplate, error)
}

// Option configures the template service.
type Option func(*Service)

// Service manages environment templates.
type Service struct {
	db           *database.Client
	orchestrator Orchestrator
}

// NewService returns a template service backed by db.
func NewService(db *database.Client, opts ...Option) *Service {
	service := &Service{db: db, orchestrator: noopOrchestrator{}}
	for _, opt := range opts {
		opt(service)
	}

	if service.orchestrator == nil {
		service.orchestrator = noopOrchestrator{}
	}

	return service
}

// WithOrchestrator configures template VM artifact preparation.
func WithOrchestrator(orchestrator Orchestrator) Option {
	return func(s *Service) {
		s.orchestrator = orchestrator
	}
}

// Create stores a template and returns its metadata.
func (s *Service) Create(ctx context.Context, req CreateRequest) (Metadata, error) {
	if err := services.ValidateOptionalKey("template", req.Key); err != nil {
		return Metadata{}, err
	}

	if len(req.Config) == 0 || !json.Valid(req.Config) {
		return Metadata{}, fmt.Errorf("%w: template config must be valid JSON", failure.ErrInvalid)
	}

	if err := schema.ValidateTemplateConfig(req.Config); err != nil {
		return Metadata{}, fmt.Errorf("%w: template config does not match schema: %w", failure.ErrInvalid, err)
	}

	preparedConfig, err := s.resolveSecrets(ctx, req.Config)
	if err != nil {
		return Metadata{}, err
	}

	if err := schema.ValidateTemplateConfig(preparedConfig); err != nil {
		return Metadata{}, fmt.Errorf("%w: resolved template config does not match schema: %w", failure.ErrInvalid, err)
	}

	templateID, err := services.GenerateID("tpl")
	if err != nil {
		return Metadata{}, err
	}

	template := Template{ID: templateID, Key: services.CopyStringPtr(req.Key), Config: append([]byte(nil), req.Config...), CreatedAt: services.Now()}

	if _, err := s.orchestrator.PrepareTemplate(ctx, ch.PrepareTemplateRequest{
		Template: ch.Template{
			ID:     template.ID,
			Key:    services.CopyStringPtr(template.Key),
			Config: preparedConfig,
		},
		Logs: req.Logs,
	}); err != nil {
		return Metadata{}, fmt.Errorf("prepare template vm: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `INSERT INTO templates (id, key, config, created_at) VALUES (?, ?, ?, ?)`, template.ID, services.OptionalStringValue(template.Key), string(template.Config), template.CreatedAt)
	if err != nil {
		_, _ = s.orchestrator.RemoveTemplate(context.Background(), template.ID)

		if database.IsConstraint(err) {
			return Metadata{}, fmt.Errorf("%w: template already exists", failure.ErrConflict)
		}

		return Metadata{}, fmt.Errorf("create template: %w", err)
	}

	return template.Metadata(), nil
}

func (s *Service) resolveSecrets(ctx context.Context, config json.RawMessage) (json.RawMessage, error) {
	secrets := secret.NewService(s.db)

	return services.SubstituteTemplateSecrets(ctx, config, func(ctx context.Context, reference string) (string, error) {
		resolved, err := secrets.GetReference(ctx, reference)
		if errors.Is(err, failure.ErrNotFound) {
			return "", fmt.Errorf("%w: secret %s not found", failure.ErrInvalid, reference)
		}

		if err != nil {
			return "", err
		}

		return resolved.Value, nil
	})
}

// Export streams a prepared template archive by ID or key.
func (s *Service) Export(ctx context.Context, templateID, key string, archive io.Writer) error {
	if archive == nil {
		return fmt.Errorf("%w: template archive writer is required", failure.ErrInvalid)
	}

	template, err := s.Get(ctx, templateID, key)
	if err != nil {
		return err
	}

	if err := s.orchestrator.ExportTemplate(ctx, ch.ExportTemplateRequest{
		Template: ch.Template{
			ID:     template.ID,
			Key:    services.CopyStringPtr(template.Key),
			Config: template.Config,
		},
		Writer: archive,
	}); err != nil {
		return fmt.Errorf("export template artifacts: %w", err)
	}

	return nil
}

// Import stores a prepared template archive under a new template ID and optional key.
//
//nolint:gocyclo // Coordinates validation, artifact restore, cleanup, and DB insert rollback.
func (s *Service) Import(ctx context.Context, req ImportRequest) (Metadata, error) {
	if err := services.ValidateOptionalKey("template", req.Key); err != nil {
		return Metadata{}, err
	}

	if req.Archive == nil {
		return Metadata{}, fmt.Errorf("%w: template archive file is required", failure.ErrInvalid)
	}

	templateID, err := services.GenerateID("tpl")
	if err != nil {
		return Metadata{}, err
	}

	imported, err := s.orchestrator.ImportTemplate(ctx, ch.ImportTemplateRequest{TemplateID: templateID, Reader: req.Archive, ContentLength: req.ArchiveSize})
	if err != nil {
		if errors.Is(err, ch.ErrInvalidTemplateArchive) {
			return Metadata{}, fmt.Errorf("%w: import template artifacts: %w", failure.ErrInvalid, err)
		}

		return Metadata{}, fmt.Errorf("import template artifacts: %w", err)
	}

	cleanupImported := true
	defer func() {
		if cleanupImported {
			_, _ = s.orchestrator.RemoveTemplate(context.Background(), templateID)
		}
	}()

	if imported.Template.ID != "" && imported.Template.ID != templateID {
		return Metadata{}, fmt.Errorf("%w: imported template id mismatch", failure.ErrInvalid)
	}

	if len(imported.Template.Config) == 0 || !json.Valid(imported.Template.Config) {
		return Metadata{}, fmt.Errorf("%w: imported template config must be valid JSON", failure.ErrInvalid)
	}

	if err := schema.ValidateTemplateConfig(imported.Template.Config); err != nil {
		return Metadata{}, fmt.Errorf("%w: imported template config does not match schema: %w", failure.ErrInvalid, err)
	}

	template := Template{ID: templateID, Key: services.CopyStringPtr(req.Key), Config: append([]byte(nil), imported.Template.Config...), CreatedAt: services.Now()}

	_, err = s.db.ExecContext(ctx, `INSERT INTO templates (id, key, config, created_at) VALUES (?, ?, ?, ?)`, template.ID, services.OptionalStringValue(template.Key), string(template.Config), template.CreatedAt)
	if err != nil {
		if database.IsConstraint(err) {
			return Metadata{}, fmt.Errorf("%w: template already exists", failure.ErrConflict)
		}

		return Metadata{}, fmt.Errorf("import template: %w", err)
	}

	cleanupImported = false

	return template.Metadata(), nil
}

// List returns template metadata ordered by creation time.
func (s *Service) List(ctx context.Context, limit int, cursor string) (services.Page[Metadata], error) {
	limit = services.NormalizeLimit(limit)

	rows, err := services.QueryPage(ctx, s.db, `SELECT id, key, created_at FROM templates`, limit, cursor)
	if err != nil {
		return services.Page[Metadata]{}, fmt.Errorf("list templates: %w", err)
	}

	defer func() { _ = rows.Close() }()

	entries := make([]Metadata, 0, limit+1)

	for rows.Next() {
		var (
			template Metadata
			key      sql.NullString
		)
		if err := rows.Scan(&template.ID, &key, &template.CreatedAt); err != nil {
			return services.Page[Metadata]{}, fmt.Errorf("scan template: %w", err)
		}

		template.Key = services.NullStringPtr(key)

		entries = append(entries, template)
	}

	if err := rows.Err(); err != nil {
		return services.Page[Metadata]{}, fmt.Errorf("iterate templates: %w", err)
	}

	return services.FromEntries(entries, limit, func(template Metadata) string { return template.CreatedAt }), nil
}

// Get returns a template by ID or key.
func (s *Service) Get(ctx context.Context, templateID, key string) (Template, error) {
	if err := services.RequireIDOrKey(templateID, key); err != nil {
		return Template{}, err
	}

	where, value := services.LookupClause(templateID, key, "id", "key")

	var (
		template    Template
		templateKey sql.NullString
		config      string
	)

	err := s.db.QueryRowContext(ctx, `SELECT id, key, config, created_at FROM templates WHERE `+where, value).Scan(&template.ID, &templateKey, &config, &template.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Template{}, fmt.Errorf("%w: template not found", failure.ErrNotFound)
	}

	if err != nil {
		return Template{}, fmt.Errorf("get template: %w", err)
	}

	template.Key = services.NullStringPtr(templateKey)
	template.Config = json.RawMessage(config)

	return template, nil
}

// Remove deletes a template by ID or key and returns the removed record.
func (s *Service) Remove(ctx context.Context, templateID, key string) (Template, error) {
	template, err := s.Get(ctx, templateID, key)
	if err != nil {
		return Template{}, err
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM templates WHERE id = ?`, template.ID); err != nil {
		if database.IsConstraint(err) {
			return Template{}, fmt.Errorf("%w: template is in use", failure.ErrConflict)
		}

		return Template{}, fmt.Errorf("remove template: %w", err)
	}

	if _, err := s.orchestrator.RemoveTemplate(ctx, template.ID); err != nil {
		return Template{}, fmt.Errorf("remove template artifacts: %w", err)
	}

	return template, nil
}

// Metadata returns the template's metadata view.
func (t Template) Metadata() Metadata {
	return Metadata{ID: t.ID, Key: services.CopyStringPtr(t.Key), CreatedAt: t.CreatedAt}
}

type noopOrchestrator struct{}

func (noopOrchestrator) PrepareTemplate(_ context.Context, req ch.PrepareTemplateRequest) (ch.PreparedTemplate, error) {
	return ch.PreparedTemplate{TemplateID: req.Template.ID, UpdatedAt: services.Now()}, nil
}

func (noopOrchestrator) RemoveTemplate(_ context.Context, templateID string) (ch.PreparedTemplate, error) {
	return ch.PreparedTemplate{TemplateID: templateID, UpdatedAt: services.Now()}, nil
}

func (noopOrchestrator) ExportTemplate(_ context.Context, _ ch.ExportTemplateRequest) error {
	return nil
}

func (noopOrchestrator) ImportTemplate(_ context.Context, req ch.ImportTemplateRequest) (ch.ImportedTemplate, error) {
	return ch.ImportedTemplate{Template: ch.Template{ID: req.TemplateID}, UpdatedAt: services.Now()}, nil
}
