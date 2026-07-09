package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/bastion-computer/bastion/core/internal/clusterdb"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/schema"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/secret"
	"github.com/bastion-computer/bastion/core/internal/services/template"
	"github.com/bastion-computer/bastion/core/internal/templatearchive"
)

const (
	secretIDPrefix   = "sec"
	templateIDPrefix = "tpl"
)

// NamespaceSelector identifies the namespace for source resource operations.
type NamespaceSelector struct {
	ID  string
	Key string
}

// CreateSecret stores a source secret in a cluster namespace.
func (s *Service) CreateSecret(ctx context.Context, namespaceSelector NamespaceSelector, req secret.CreateRequest) (secret.Metadata, error) {
	namespace, err := s.resolveNamespace(ctx, namespaceSelector)
	if err != nil {
		return secret.Metadata{}, err
	}

	if err := validateSecretCreateRequest(req); err != nil {
		return secret.Metadata{}, err
	}

	secretID, err := services.GenerateID(secretIDPrefix)
	if err != nil {
		return secret.Metadata{}, err
	}

	created := secret.Secret{ID: secretID, Key: services.CopyStringPtr(req.Key), Value: req.Value, CreatedAt: services.Now()}

	_, err = s.db.Exec(ctx, `INSERT INTO cluster_secrets (id, namespace_id, key, value, created_at) VALUES ($1, $2, $3, $4, $5)`, created.ID, namespace.ID, services.OptionalStringValue(created.Key), created.Value, created.CreatedAt)
	if err != nil {
		if clusterdb.IsConstraint(err) {
			return secret.Metadata{}, fmt.Errorf("%w: secret already exists", failure.ErrConflict)
		}

		return secret.Metadata{}, fmt.Errorf("create cluster secret: %w", err)
	}

	return secretMetadata(created), nil
}

// ListSecrets returns source secret metadata scoped to a namespace.
func (s *Service) ListSecrets(ctx context.Context, namespaceSelector NamespaceSelector, limit int, cursor string) (services.Page[secret.Metadata], error) {
	return listMetadata(ctx, s, namespaceSelector, limit, cursor, secretMetadataListQuery, buildSecretMetadata, secretMetadataCursor)
}

// GetSecret returns a source secret scoped to a namespace by ID or key.
func (s *Service) GetSecret(ctx context.Context, namespaceSelector NamespaceSelector, secretID, key string) (secret.Secret, error) {
	namespace, err := s.resolveNamespace(ctx, namespaceSelector)
	if err != nil {
		return secret.Secret{}, err
	}

	return s.getSecretInNamespace(ctx, namespace.ID, secretID, key)
}

// RemoveSecret removes a source secret scoped to a namespace.
func (s *Service) RemoveSecret(ctx context.Context, namespaceSelector NamespaceSelector, secretID, key string) (secret.Metadata, error) {
	removed, err := s.GetSecret(ctx, namespaceSelector, secretID, key)
	if err != nil {
		return secret.Metadata{}, err
	}

	if _, err := s.db.Exec(ctx, `DELETE FROM cluster_secrets WHERE id = $1`, removed.ID); err != nil {
		return secret.Metadata{}, fmt.Errorf("remove cluster secret: %w", err)
	}

	return secretMetadata(removed), nil
}

// CreateTemplate creates a source template and a temporary node derivative, then stores its exported archive.
//
//nolint:gocyclo // Coordinates validation, derivative provisioning, archive persistence, streamed progress, and cleanup.
func (s *Service) CreateTemplate(ctx context.Context, namespaceSelector NamespaceSelector, req template.CreateRequest) (template.Metadata, error) {
	if err := writeClusterProgress(req.Logs, "resolving namespace"); err != nil {
		return template.Metadata{}, err
	}

	namespace, err := s.resolveNamespace(ctx, namespaceSelector)
	if err != nil {
		return template.Metadata{}, err
	}

	if err := writeClusterProgress(req.Logs, "validating template config"); err != nil {
		return template.Metadata{}, err
	}

	if err := validateTemplateConfig(req.Key, req.Config, "template"); err != nil {
		return template.Metadata{}, err
	}

	if err := writeClusterProgress(req.Logs, "checking template archive storage"); err != nil {
		return template.Metadata{}, err
	}

	if err := s.requireArchiveStore(); err != nil {
		return template.Metadata{}, err
	}

	if err := writeClusterProgress(req.Logs, "resolving cluster base"); err != nil {
		return template.Metadata{}, err
	}

	baseRecord, err := s.requireBaseRecord(ctx)
	if err != nil {
		return template.Metadata{}, err
	}

	if err := writeClusterProgress(req.Logs, "selecting cluster node"); err != nil {
		return template.Metadata{}, err
	}

	node, err := s.selectNode(ctx)
	if err != nil {
		return template.Metadata{}, err
	}

	templateID, err := services.GenerateID(templateIDPrefix)
	if err != nil {
		return template.Metadata{}, err
	}

	createdAt := services.Now()
	sourceTemplate := template.Template{ID: templateID, Key: services.CopyStringPtr(req.Key), Config: append(json.RawMessage(nil), req.Config...), BaseContentAddress: baseRecord.Metadata.ContentAddress, CreatedAt: createdAt}
	archiveKey := templateArchiveObjectKey(namespace.ID, sourceTemplate.ID)
	derivatives := derivativeCleanup{service: s, nodeURL: node.URL}
	cleanupDerivatives := true

	defer func() {
		if cleanupDerivatives {
			_ = derivatives.cleanup(context.Background())
		}
	}()

	if err := writeClusterProgress(req.Logs, "preparing derivative secrets"); err != nil {
		return template.Metadata{}, err
	}

	derivativeConfig, sourceDerivativeSecrets, err := s.createDerivativeSecrets(ctx, namespace.ID, node, req.Config, &derivatives, req.Logs)
	if err != nil {
		return template.Metadata{}, err
	}

	if err := writeClusterProgress(req.Logs, "creating derivative template on cluster node %s", node.ID); err != nil {
		return template.Metadata{}, err
	}

	createdDerivative, err := s.nodeClient.CreateTemplate(ctx, node.URL, template.CreateRequest{Config: derivativeConfig, Logs: req.Logs})
	if err != nil {
		return template.Metadata{}, fmt.Errorf("%w: create derivative template on cluster node %s: %w", failure.ErrFailedDependency, node.ID, err)
	}

	derivatives.templateID = createdDerivative.ID

	derivativeArchive, err := createTempFile("bastion-cluster-derivative-*.tar.zst")
	if err != nil {
		return template.Metadata{}, err
	}
	defer derivativeArchive.cleanup()

	if err := writeClusterProgress(req.Logs, "exporting derivative template archive from cluster node %s", node.ID); err != nil {
		return template.Metadata{}, err
	}

	if err := s.nodeClient.ExportTemplate(ctx, node.URL, createdDerivative.ID, derivativeArchive.file); err != nil {
		return template.Metadata{}, fmt.Errorf("%w: export derivative template from cluster node %s: %w", failure.ErrFailedDependency, node.ID, err)
	}

	if err := writeClusterProgress(req.Logs, "cleaning up derivative resources"); err != nil {
		return template.Metadata{}, err
	}

	if err := derivatives.cleanup(ctx); err != nil {
		return template.Metadata{}, err
	}

	cleanupDerivatives = false

	if err := s.insertTemplateWithArchive(ctx, namespace.ID, sourceTemplate, archiveKey, derivativeArchive.file, node.ID, createdDerivative.ID, sourceDerivativeSecrets, req.Logs); err != nil {
		return template.Metadata{}, err
	}

	return templateMetadata(sourceTemplate), nil
}

// ImportTemplate stores an uploaded prepared template archive as a source template in a namespace.
//
//nolint:gocyclo // Coordinates upload validation, base matching, archive rewrite, storage, and DB insert.
func (s *Service) ImportTemplate(ctx context.Context, namespaceSelector NamespaceSelector, req template.ImportRequest) (template.Metadata, error) {
	namespace, err := s.resolveNamespace(ctx, namespaceSelector)
	if err != nil {
		return template.Metadata{}, err
	}

	if err := services.ValidateOptionalKey("template", req.Key); err != nil {
		return template.Metadata{}, err
	}

	if req.Archive == nil {
		return template.Metadata{}, fmt.Errorf("%w: template archive file is required", failure.ErrInvalid)
	}

	if err := s.requireArchiveStore(); err != nil {
		return template.Metadata{}, err
	}

	baseRecord, err := s.requireBaseRecord(ctx)
	if err != nil {
		return template.Metadata{}, err
	}

	sourceArchive, err := createTempFile("bastion-cluster-import-*.tar.zst")
	if err != nil {
		return template.Metadata{}, err
	}
	defer sourceArchive.cleanup()

	if _, err := io.Copy(sourceArchive.file, req.Archive); err != nil {
		return template.Metadata{}, fmt.Errorf("read template archive upload: %w", err)
	}

	if _, err := sourceArchive.file.Seek(0, io.SeekStart); err != nil {
		return template.Metadata{}, fmt.Errorf("rewind uploaded template archive: %w", err)
	}

	archiveTemplate, err := templatearchive.ReadTemplate(ctx, sourceArchive.file)
	if err != nil {
		if errors.Is(err, templatearchive.ErrInvalid) {
			return template.Metadata{}, fmt.Errorf("%w: import template archive: %w", failure.ErrInvalid, err)
		}

		return template.Metadata{}, fmt.Errorf("import template archive: %w", err)
	}

	if archiveTemplate.BaseContentAddress != baseRecord.Metadata.ContentAddress {
		return template.Metadata{}, fmt.Errorf("%w: imported template base content address %s does not match cluster base %s", failure.ErrInvalid, archiveTemplate.BaseContentAddress, baseRecord.Metadata.ContentAddress)
	}

	if err := validateTemplateConfig(req.Key, archiveTemplate.Config, "imported template"); err != nil {
		return template.Metadata{}, err
	}

	templateID, err := services.GenerateID(templateIDPrefix)
	if err != nil {
		return template.Metadata{}, err
	}

	sourceTemplate := template.Template{ID: templateID, Key: services.CopyStringPtr(req.Key), Config: append(json.RawMessage(nil), archiveTemplate.Config...), BaseContentAddress: archiveTemplate.BaseContentAddress, CreatedAt: services.Now()}
	archiveKey := templateArchiveObjectKey(namespace.ID, sourceTemplate.ID)

	if err := s.insertTemplateWithArchive(ctx, namespace.ID, sourceTemplate, archiveKey, sourceArchive.file, "", "", nil, nil); err != nil {
		return template.Metadata{}, err
	}

	return templateMetadata(sourceTemplate), nil
}

// ListTemplates returns source template metadata scoped to a namespace.
func (s *Service) ListTemplates(ctx context.Context, namespaceSelector NamespaceSelector, limit int, cursor string) (services.Page[template.Metadata], error) {
	return listMetadata(ctx, s, namespaceSelector, limit, cursor, templateMetadataListQuery, buildTemplateMetadata, templateMetadataCursor)
}

// GetTemplate returns a source template scoped to a namespace by ID or key.
func (s *Service) GetTemplate(ctx context.Context, namespaceSelector NamespaceSelector, templateID, key string) (template.Template, error) {
	namespace, err := s.resolveNamespace(ctx, namespaceSelector)
	if err != nil {
		return template.Template{}, err
	}

	resource, _, _, _, err := s.getTemplateInNamespace(ctx, namespace.ID, templateID, key)

	return resource, err
}

// ExportTemplate streams an already stored source template archive.
func (s *Service) ExportTemplate(ctx context.Context, namespaceSelector NamespaceSelector, templateID, key string, archive io.Writer) error {
	if archive == nil {
		return fmt.Errorf("%w: template archive writer is required", failure.ErrInvalid)
	}

	namespace, err := s.resolveNamespace(ctx, namespaceSelector)
	if err != nil {
		return err
	}

	if err := s.requireArchiveStore(); err != nil {
		return err
	}

	_, archiveKey, _, _, err := s.getTemplateInNamespace(ctx, namespace.ID, templateID, key)
	if err != nil {
		return err
	}

	if err := s.archiveStore.Get(ctx, archiveKey, archive); err != nil {
		return fmt.Errorf("export template archive: %w", err)
	}

	return nil
}

// RemoveTemplate removes a source template and its stored archive.
func (s *Service) RemoveTemplate(ctx context.Context, namespaceSelector NamespaceSelector, templateID, key string) (template.Template, error) {
	namespace, err := s.resolveNamespace(ctx, namespaceSelector)
	if err != nil {
		return template.Template{}, err
	}

	if err := s.requireArchiveStore(); err != nil {
		return template.Template{}, err
	}

	removed, archiveKey, _, _, err := s.getTemplateInNamespace(ctx, namespace.ID, templateID, key)
	if err != nil {
		return template.Template{}, err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return template.Template{}, fmt.Errorf("begin cluster template removal: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM cluster_templates WHERE id = $1`, removed.ID); err != nil {
		if clusterdb.IsForeignKeyViolation(err) {
			return template.Template{}, fmt.Errorf("%w: template is in use", failure.ErrFailedDependency)
		}

		return template.Template{}, fmt.Errorf("remove cluster template: %w", err)
	}

	if err := s.archiveStore.Delete(ctx, archiveKey); err != nil {
		return template.Template{}, fmt.Errorf("remove template archive: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return template.Template{}, fmt.Errorf("commit cluster template removal: %w", err)
	}

	return removed, nil
}

func validateSecretCreateRequest(req secret.CreateRequest) error {
	if err := services.ValidateOptionalKey("secret", req.Key); err != nil {
		return err
	}

	if req.Key != nil && strings.HasPrefix(*req.Key, secretIDPrefix+"_") {
		return fmt.Errorf("%w: secret key cannot use reserved %s_ prefix", failure.ErrInvalid, secretIDPrefix)
	}

	if req.Value == "" {
		return fmt.Errorf("%w: secret value is required", failure.ErrInvalid)
	}

	return nil
}

func validateTemplateConfig(key *string, config json.RawMessage, label string) error {
	if err := services.ValidateOptionalKey("template", key); err != nil {
		return err
	}

	if len(config) == 0 || !json.Valid(config) {
		return fmt.Errorf("%w: %s config must be valid JSON", failure.ErrInvalid, label)
	}

	if err := schema.ValidateTemplateConfig(config); err != nil {
		return fmt.Errorf("%w: %s config does not match schema: %w", failure.ErrInvalid, label, err)
	}

	return nil
}

func (s *Service) resolveNamespace(ctx context.Context, selector NamespaceSelector) (Namespace, error) {
	switch {
	case selector.ID == "" && selector.Key == "":
		return Namespace{}, fmt.Errorf("%w: namespace is required", failure.ErrInvalid)
	case selector.ID != "" && selector.Key != "":
		return Namespace{}, fmt.Errorf("%w: specify exactly one of namespace-id or namespace-key", failure.ErrInvalid)
	}

	return s.GetNamespace(ctx, selector.ID, selector.Key)
}

func (s *Service) requireArchiveStore() error {
	if s.archiveStore == nil {
		return fmt.Errorf("%w: template archive storage is not configured", failure.ErrFailedDependency)
	}

	return nil
}

func (s *Service) requireBaseRecord(ctx context.Context) (baseRecord, error) {
	record, err := s.getBaseRecord(ctx)
	if errors.Is(err, failure.ErrNotFound) {
		return baseRecord{}, fmt.Errorf("%w: base not found", failure.ErrFailedDependency)
	}

	return record, err
}

func (s *Service) selectNode(ctx context.Context) (Node, error) {
	nodes, err := s.allNodes(ctx)
	if err != nil {
		return Node{}, err
	}

	if len(nodes) == 0 {
		return Node{}, fmt.Errorf("%w: no cluster nodes are registered", failure.ErrFailedDependency)
	}

	return nodes[0], nil
}

type derivativeSecretMapping struct {
	sourceID     string
	derivativeID string
}

func (s *Service) createDerivativeSecrets(ctx context.Context, namespaceID string, node Node, config json.RawMessage, cleanup *derivativeCleanup, logs io.Writer) (json.RawMessage, []derivativeSecretMapping, error) {
	mappingsBySourceID := map[string]string{}
	mappings := []derivativeSecretMapping{}

	rewritten, err := services.RewriteTemplateSecretReferences(ctx, config, func(ctx context.Context, reference string) (string, error) {
		sourceSecret, err := s.getSecretReference(ctx, namespaceID, reference)
		if errors.Is(err, failure.ErrNotFound) {
			return "", fmt.Errorf("%w: secret %s not found", failure.ErrInvalid, reference)
		}

		if err != nil {
			return "", err
		}

		if derivativeID, ok := mappingsBySourceID[sourceSecret.ID]; ok {
			return derivativeID, nil
		}

		if err := writeClusterProgress(logs, "creating derivative secret on cluster node %s", node.ID); err != nil {
			return "", err
		}

		created, err := s.nodeClient.CreateSecret(ctx, node.URL, secret.CreateRequest{Value: sourceSecret.Value})
		if err != nil {
			return "", fmt.Errorf("%w: create derivative secret on cluster node %s: %w", failure.ErrFailedDependency, node.ID, err)
		}

		cleanup.secretIDs = append(cleanup.secretIDs, created.ID)
		mappingsBySourceID[sourceSecret.ID] = created.ID
		mappings = append(mappings, derivativeSecretMapping{sourceID: sourceSecret.ID, derivativeID: created.ID})

		return created.ID, nil
	})
	if err != nil {
		return nil, nil, err
	}

	if err := schema.ValidateTemplateConfig(rewritten); err != nil {
		return nil, nil, fmt.Errorf("%w: derivative template config does not match schema: %w", failure.ErrInvalid, err)
	}

	return rewritten, mappings, nil
}

func (s *Service) getSecretReference(ctx context.Context, namespaceID, reference string) (secret.Secret, error) {
	if strings.HasPrefix(reference, secretIDPrefix+"_") {
		return s.getSecretInNamespace(ctx, namespaceID, reference, "")
	}

	return s.getSecretInNamespace(ctx, namespaceID, "", reference)
}

func (s *Service) getSecretInNamespace(ctx context.Context, namespaceID, secretID, key string) (secret.Secret, error) {
	if err := services.RequireIDOrKey(secretID, key); err != nil {
		return secret.Secret{}, err
	}

	var (
		resource  secret.Secret
		secretKey sql.NullString
	)

	var err error
	if secretID != "" {
		err = s.db.QueryRow(ctx, `SELECT id, key, value, created_at FROM cluster_secrets WHERE namespace_id = $1 AND id = $2`, namespaceID, secretID).Scan(&resource.ID, &secretKey, &resource.Value, &resource.CreatedAt)
	} else {
		err = s.db.QueryRow(ctx, `SELECT id, key, value, created_at FROM cluster_secrets WHERE namespace_id = $1 AND key = $2`, namespaceID, key).Scan(&resource.ID, &secretKey, &resource.Value, &resource.CreatedAt)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return secret.Secret{}, fmt.Errorf("%w: secret not found", failure.ErrNotFound)
	}

	if err != nil {
		return secret.Secret{}, fmt.Errorf("get cluster secret: %w", err)
	}

	resource.Key = services.NullStringPtr(secretKey)

	return resource, nil
}

func (s *Service) insertTemplateWithArchive(ctx context.Context, namespaceID string, resource template.Template, archiveKey string, sourceArchive *os.File, nodeID, derivativeTemplateID string, derivativeSecrets []derivativeSecretMapping, logs io.Writer) error {
	rewrittenArchive, err := createTempFile("bastion-cluster-source-*.tar.zst")
	if err != nil {
		return err
	}
	defer rewrittenArchive.cleanup()

	if _, err := sourceArchive.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind template archive: %w", err)
	}

	if err := validateTemplateArchiveBase(ctx, sourceArchive, resource.BaseContentAddress); err != nil {
		return err
	}

	archiveTemplate := templatearchive.Template{ID: resource.ID, Key: services.CopyStringPtr(resource.Key), Config: resource.Config, BaseContentAddress: resource.BaseContentAddress}

	if err := writeClusterProgress(logs, "rewriting source template archive"); err != nil {
		return err
	}

	if err := templatearchive.RewriteTemplate(ctx, sourceArchive, rewrittenArchive.file, archiveTemplate); err != nil {
		return fmt.Errorf("rewrite template archive manifest: %w", err)
	}

	if _, err := rewrittenArchive.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind source template archive: %w", err)
	}

	if err := writeClusterProgress(logs, "storing source template archive"); err != nil {
		return err
	}

	if err := s.archiveStore.Put(ctx, archiveKey, rewrittenArchive.file); err != nil {
		return fmt.Errorf("store template archive: %w", err)
	}

	archiveStored := true
	defer func() {
		if archiveStored {
			_ = s.archiveStore.Delete(context.Background(), archiveKey)
		}
	}()

	if err := writeClusterProgress(logs, "recording source template"); err != nil {
		return err
	}

	if err := s.insertTemplate(ctx, namespaceID, resource, archiveKey, nodeID, derivativeTemplateID, derivativeSecrets); err != nil {
		return err
	}

	archiveStored = false

	return nil
}

func validateTemplateArchiveBase(ctx context.Context, archive *os.File, baseContentAddress string) error {
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind template archive: %w", err)
	}

	archiveTemplate, err := templatearchive.ReadTemplate(ctx, archive)
	if err != nil {
		if errors.Is(err, templatearchive.ErrInvalid) {
			return fmt.Errorf("%w: import template archive: %w", failure.ErrInvalid, err)
		}

		return fmt.Errorf("import template archive: %w", err)
	}

	if archiveTemplate.BaseContentAddress != baseContentAddress {
		return fmt.Errorf("%w: template archive base content address %s does not match cluster base %s", failure.ErrInvalid, archiveTemplate.BaseContentAddress, baseContentAddress)
	}

	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind template archive: %w", err)
	}

	return nil
}

func (s *Service) insertTemplate(ctx context.Context, namespaceID string, resource template.Template, archiveKey, nodeID, derivativeTemplateID string, derivativeSecrets []derivativeSecretMapping) error {
	_, err := s.db.Exec(ctx, `INSERT INTO cluster_templates (id, namespace_id, key, config, base_content_address, archive_key, node_id, derivative_template_id, created_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, resource.ID, namespaceID, services.OptionalStringValue(resource.Key), string(resource.Config), resource.BaseContentAddress, archiveKey, optionalEmptyString(nodeID), optionalEmptyString(derivativeTemplateID), resource.CreatedAt)
	if err != nil {
		if clusterdb.IsConstraint(err) {
			return fmt.Errorf("%w: template already exists", failure.ErrConflict)
		}

		return fmt.Errorf("create cluster template: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_, _ = s.db.Exec(context.Background(), `DELETE FROM cluster_templates WHERE id = $1`, resource.ID)
		}
	}()

	for position, mapping := range derivativeSecrets {
		if _, err := s.db.Exec(ctx, `INSERT INTO cluster_template_derivative_secrets (template_id, source_secret_id, derivative_secret_id, position) VALUES ($1, $2, $3, $4)`, resource.ID, mapping.sourceID, mapping.derivativeID, position); err != nil {
			return fmt.Errorf("record template derivative secret: %w", err)
		}
	}

	committed = true

	return nil
}

func optionalEmptyString(value string) any {
	if value == "" {
		return nil
	}

	return value
}

func (s *Service) getTemplateInNamespace(ctx context.Context, namespaceID, templateID, key string) (template.Template, string, *string, *string, error) {
	if err := services.RequireIDOrKey(templateID, key); err != nil {
		return template.Template{}, "", nil, nil, err
	}

	var (
		resource             template.Template
		templateKey          sql.NullString
		config               string
		archiveKey           string
		nodeID               sql.NullString
		derivativeTemplateID sql.NullString
	)

	var err error
	if templateID != "" {
		err = s.db.QueryRow(ctx, `SELECT id, key, config, base_content_address, archive_key, node_id, derivative_template_id, created_at FROM cluster_templates WHERE namespace_id = $1 AND id = $2`, namespaceID, templateID).Scan(&resource.ID, &templateKey, &config, &resource.BaseContentAddress, &archiveKey, &nodeID, &derivativeTemplateID, &resource.CreatedAt)
	} else {
		err = s.db.QueryRow(ctx, `SELECT id, key, config, base_content_address, archive_key, node_id, derivative_template_id, created_at FROM cluster_templates WHERE namespace_id = $1 AND key = $2`, namespaceID, key).Scan(&resource.ID, &templateKey, &config, &resource.BaseContentAddress, &archiveKey, &nodeID, &derivativeTemplateID, &resource.CreatedAt)
	}

	if errors.Is(err, pgx.ErrNoRows) {
		return template.Template{}, "", nil, nil, fmt.Errorf("%w: template not found", failure.ErrNotFound)
	}

	if err != nil {
		return template.Template{}, "", nil, nil, fmt.Errorf("get cluster template: %w", err)
	}

	resource.Key = services.NullStringPtr(templateKey)
	resource.Config = json.RawMessage(config)

	return resource, archiveKey, services.NullStringPtr(nodeID), services.NullStringPtr(derivativeTemplateID), nil
}

type metadataListQuery struct {
	all         string
	afterCursor string
	listLabel   string
	scanLabel   string
}

var (
	secretMetadataListQuery = metadataListQuery{
		all:         `SELECT id, key, '' AS base_content_address, created_at FROM cluster_secrets WHERE namespace_id = $1 ORDER BY created_at LIMIT $2`,
		afterCursor: `SELECT id, key, '' AS base_content_address, created_at FROM cluster_secrets WHERE namespace_id = $1 AND created_at > $2 ORDER BY created_at LIMIT $3`,
		listLabel:   "secrets",
		scanLabel:   "secret",
	}
	templateMetadataListQuery = metadataListQuery{
		all:         `SELECT id, key, base_content_address, created_at FROM cluster_templates WHERE namespace_id = $1 ORDER BY created_at LIMIT $2`,
		afterCursor: `SELECT id, key, base_content_address, created_at FROM cluster_templates WHERE namespace_id = $1 AND created_at > $2 ORDER BY created_at LIMIT $3`,
		listLabel:   "templates",
		scanLabel:   "template",
	}
)

func listMetadata[T any](ctx context.Context, service *Service, namespaceSelector NamespaceSelector, limit int, cursor string, query metadataListQuery, build func(string, *string, string, string) T, cursorValue func(T) string) (services.Page[T], error) {
	namespace, err := service.resolveNamespace(ctx, namespaceSelector)
	if err != nil {
		return services.Page[T]{}, err
	}

	return listClusterMetadata(ctx, service.db, namespace.ID, limit, cursor, query, build, cursorValue)
}

func listClusterMetadata[T any](ctx context.Context, db *clusterdb.Client, namespaceID string, limit int, cursor string, query metadataListQuery, build func(string, *string, string, string) T, cursorValue func(T) string) (services.Page[T], error) {
	limit = services.NormalizeLimit(limit)

	rows, err := queryClusterMetadataRows(ctx, db, namespaceID, limit, cursor, query)
	if err != nil {
		return services.Page[T]{}, fmt.Errorf("list cluster %s: %w", query.listLabel, err)
	}
	defer rows.Close()

	entries, err := scanResourceMetadataRows(rows, limit+1, query, build)
	if err != nil {
		return services.Page[T]{}, err
	}

	return services.FromEntries(entries, limit, cursorValue), nil
}

func queryClusterMetadataRows(ctx context.Context, db *clusterdb.Client, namespaceID string, limit int, cursor string, query metadataListQuery) (pgx.Rows, error) {
	if cursor == "" {
		return db.Query(ctx, query.all, namespaceID, limit+1)
	}

	return db.Query(ctx, query.afterCursor, namespaceID, cursor, limit+1)
}

func scanResourceMetadataRows[T any](rows pgx.Rows, capacity int, query metadataListQuery, build func(string, *string, string, string) T) ([]T, error) {
	entries := make([]T, 0, capacity)

	for rows.Next() {
		var (
			id                 string
			key                sql.NullString
			baseContentAddress string
			createdAt          string
		)

		if err := rows.Scan(&id, &key, &baseContentAddress, &createdAt); err != nil {
			return nil, fmt.Errorf("scan cluster %s: %w", query.scanLabel, err)
		}

		entries = append(entries, build(id, services.NullStringPtr(key), baseContentAddress, createdAt))
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cluster %s: %w", query.listLabel, err)
	}

	return entries, nil
}

func buildSecretMetadata(id string, key *string, baseContentAddress, createdAt string) secret.Metadata {
	_ = baseContentAddress

	return secret.Metadata{ID: id, Key: key, CreatedAt: createdAt}
}

func secretMetadataCursor(entry secret.Metadata) string {
	return entry.CreatedAt
}

func buildTemplateMetadata(id string, key *string, baseContentAddress, createdAt string) template.Metadata {
	return template.Metadata{ID: id, Key: key, BaseContentAddress: baseContentAddress, CreatedAt: createdAt}
}

func templateMetadataCursor(entry template.Metadata) string {
	return entry.CreatedAt
}

type derivativeCleanup struct {
	service    *Service
	nodeURL    string
	templateID string
	secretIDs  []string
}

func (c *derivativeCleanup) cleanup(ctx context.Context) error {
	var joined error

	if c.templateID != "" {
		if err := c.service.nodeClient.RemoveTemplate(ctx, c.nodeURL, c.templateID); err != nil {
			joined = errors.Join(joined, fmt.Errorf("remove derivative template: %w", err))
		}

		c.templateID = ""
	}

	for _, secretID := range c.secretIDs {
		if err := c.service.nodeClient.RemoveSecret(ctx, c.nodeURL, secretID); err != nil {
			joined = errors.Join(joined, fmt.Errorf("remove derivative secret %s: %w", secretID, err))
		}
	}

	c.secretIDs = nil

	if joined != nil {
		return fmt.Errorf("%w: cleanup derivative resources: %w", failure.ErrFailedDependency, joined)
	}

	return nil
}

type tempFile struct {
	file *os.File
	path string
}

func createTempFile(pattern string) (tempFile, error) {
	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return tempFile{}, fmt.Errorf("create temporary template archive: %w", err)
	}

	return tempFile{file: file, path: file.Name()}, nil
}

func (f tempFile) cleanup() {
	if f.file != nil {
		_ = f.file.Close()
	}

	if f.path != "" {
		_ = os.Remove(f.path)
	}
}

func templateArchiveObjectKey(namespaceID, templateID string) string {
	return "templates/" + namespaceID + "/" + templateID + ".tar.zst"
}

func secretMetadata(resource secret.Secret) secret.Metadata {
	return secret.Metadata{ID: resource.ID, Key: services.CopyStringPtr(resource.Key), CreatedAt: resource.CreatedAt}
}

func templateMetadata(resource template.Template) template.Metadata {
	return template.Metadata{ID: resource.ID, Key: services.CopyStringPtr(resource.Key), BaseContentAddress: resource.BaseContentAddress, CreatedAt: resource.CreatedAt}
}
