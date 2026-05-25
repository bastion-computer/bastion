// Package environment manages Bastion environment records.
package environment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/schema"
	"github.com/bastion-computer/bastion/core/internal/services"
)

// Environment describes a managed opencode environment.
type Environment struct {
	ID         string   `json:"id"`
	Key        *string  `json:"key,omitempty"`
	Status     string   `json:"status"`
	TemplateID string   `json:"templateId"`
	Tags       []string `json:"tags"`
	CreatedAt  string   `json:"createdAt"`
	UpdatedAt  string   `json:"updatedAt"`
	LastError  string   `json:"lastError,omitempty"`
}

// SSHConnection contains private connection metadata for API-managed SSH.
type SSHConnection struct {
	Host    string
	Port    int
	User    string
	KeyPath string
}

type environmentRecord struct {
	Environment
	SSHConnection
	VMLastError string
}

// CreateRequest contains the fields needed to create an environment.
type CreateRequest struct {
	Key         *string   `json:"key,omitempty"`
	TemplateID  string    `json:"templateId,omitempty"`
	TemplateKey string    `json:"templateKey,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Logs        io.Writer `json:"-"`
}

// Stream event types used by POST /v1/environments.
const (
	StreamEventLog    = "log"
	StreamEventResult = "result"
	StreamEventError  = "error"
)

// CreateStreamEvent is one line in a streamed environment creation response.
type CreateStreamEvent struct {
	Type        string       `json:"type"`
	Log         string       `json:"log,omitempty"`
	Environment *Environment `json:"environment,omitempty"`
	Error       string       `json:"error,omitempty"`
	Status      int          `json:"status,omitempty"`
}

// Orchestrator launches, reconciles, and removes environment VMs.
type Orchestrator interface {
	Launch(context.Context, ch.LaunchRequest) (ch.VM, error)
	State(context.Context, string) (ch.VM, error)
	Remove(context.Context, string) (ch.VM, error)
}

// Option configures the environment service.
type Option func(*Service)

// Service manages environment records.
type Service struct {
	db           *database.Client
	orchestrator Orchestrator
}

// NewService returns an environment service backed by db.
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

// WithOrchestrator configures the VM orchestrator used by environment lifecycle methods.
func WithOrchestrator(orchestrator Orchestrator) Option {
	return func(s *Service) {
		s.orchestrator = orchestrator
	}
}

// Create stores an environment from a template.
func (s *Service) Create(ctx context.Context, req CreateRequest) (Environment, error) {
	if err := services.RequireIDOrKey(req.TemplateID, req.TemplateKey); err != nil {
		return Environment{}, err
	}

	if err := services.ValidateOptionalKey("environment", req.Key); err != nil {
		return Environment{}, err
	}

	if err := validateTags(req.Tags); err != nil {
		return Environment{}, err
	}

	template, err := s.resolveTemplate(ctx, req.TemplateID, req.TemplateKey)
	if err != nil {
		return Environment{}, err
	}

	template.Config, err = substituteTemplateEnvironment(template.Config)
	if err != nil {
		return Environment{}, err
	}

	if err := schema.ValidateTemplateConfig(template.Config); err != nil {
		return Environment{}, fmt.Errorf("%w: resolved template config does not match schema: %w", failure.ErrInvalid, err)
	}

	environmentID, err := services.GenerateID("env")
	if err != nil {
		return Environment{}, err
	}

	now := services.Now()
	environment := Environment{ID: environmentID, Key: services.CopyStringPtr(req.Key), Status: ch.StateCreating, TemplateID: template.ID, Tags: copyTags(req.Tags), CreatedAt: now, UpdatedAt: now}

	if err := s.createRecord(ctx, environment); err != nil {
		return Environment{}, err
	}

	vm, err := s.orchestrator.Launch(ctx, ch.LaunchRequest{
		EnvironmentID: environment.ID,
		Template: ch.Template{
			ID:     template.ID,
			Key:    services.CopyStringPtr(template.Key),
			Config: template.Config,
		},
		Logs: req.Logs,
	})
	if err != nil {
		recordCtx, cancel := launchFailureContext(ctx)
		defer cancel()

		if recordErr := s.recordLaunchFailure(recordCtx, environment.ID, vm, err); recordErr != nil {
			err = errors.Join(err, recordErr)
		}

		return Environment{}, fmt.Errorf("launch environment vm: %w", err)
	}

	if err := s.saveVM(ctx, vm); err != nil {
		_, _ = s.orchestrator.Remove(context.Background(), environment.ID)
		_ = s.updateStatus(ctx, environment.ID, ch.StateError, err.Error())

		return Environment{}, err
	}

	if err := s.updateStatus(ctx, environment.ID, statusFromVM(vm), vm.LastError); err != nil {
		_, _ = s.orchestrator.Remove(context.Background(), environment.ID)

		return Environment{}, err
	}

	return s.Get(ctx, environment.ID)
}

func (s *Service) createRecord(ctx context.Context, environment Environment) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin create environment: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	_, err = tx.ExecContext(ctx, `INSERT INTO environments (id, key, status, template_id, created_at, updated_at, last_error) VALUES (?, ?, ?, ?, ?, ?, ?)`, environment.ID, services.OptionalStringValue(environment.Key), environment.Status, environment.TemplateID, environment.CreatedAt, environment.UpdatedAt, "")
	if err != nil {
		if database.IsConstraint(err) {
			return fmt.Errorf("%w: environment already exists", failure.ErrConflict)
		}

		return fmt.Errorf("create environment: %w", err)
	}

	for position, tag := range environment.Tags {
		if _, err := tx.ExecContext(ctx, `INSERT INTO environment_tags (environment_id, tag, position) VALUES (?, ?, ?)`, environment.ID, tag, position); err != nil {
			return fmt.Errorf("create environment tag: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit create environment: %w", err)
	}

	committed = true

	return nil
}

func launchFailureContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx.Err() == nil {
		return ctx, func() {}
	}

	return context.WithTimeout(context.Background(), 5*time.Second)
}

// List returns environments ordered by creation time.
func (s *Service) List(ctx context.Context, limit int, cursor string, tags []string) (services.Page[Environment], error) {
	limit = services.NormalizeLimit(limit)

	if err := validateTags(tags); err != nil {
		return services.Page[Environment]{}, err
	}

	query := environmentSelectQuery()
	filters := make([]string, 0, len(tags)+1)
	args := make([]any, 0, len(tags)+2)

	if cursor != "" {
		filters = append(filters, "e.created_at > ?")
		args = append(args, cursor)
	}

	for index, tag := range uniqueTags(tags) {
		alias := fmt.Sprintf("tag_filter_%d", index)
		filters = append(filters, fmt.Sprintf("EXISTS (SELECT 1 FROM environment_tags %s WHERE %s.environment_id = e.id AND %s.tag = ?)", alias, alias, alias))
		args = append(args, tag)
	}

	if len(filters) > 0 {
		query += ` WHERE ` + strings.Join(filters, " AND ")
	}

	query += ` ORDER BY e.created_at LIMIT ?`

	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return services.Page[Environment]{}, fmt.Errorf("list environments: %w", err)
	}

	defer func() { _ = rows.Close() }()

	entries := make([]Environment, 0, limit+1)

	for rows.Next() {
		record, err := scanEnvironmentRecord(rows)
		if err != nil {
			return services.Page[Environment]{}, fmt.Errorf("scan environment: %w", err)
		}

		entries = append(entries, record.Environment)
	}

	if err := rows.Err(); err != nil {
		return services.Page[Environment]{}, fmt.Errorf("iterate environments: %w", err)
	}

	if err := s.loadTags(ctx, entries); err != nil {
		return services.Page[Environment]{}, err
	}

	for i := range entries {
		reconciled, err := s.reconcile(ctx, entries[i])
		if err != nil {
			return services.Page[Environment]{}, err
		}

		entries[i] = reconciled
	}

	return services.FromEntries(entries, limit, func(environment Environment) string { return environment.CreatedAt }), nil
}

// Get returns an environment by ID.
func (s *Service) Get(ctx context.Context, environmentID string) (Environment, error) {
	return s.getByIDOrKey(ctx, environmentID, "")
}

// GetByKey returns an environment by key.
func (s *Service) GetByKey(ctx context.Context, key string) (Environment, error) {
	return s.getByIDOrKey(ctx, "", key)
}

func (s *Service) getByIDOrKey(ctx context.Context, environmentID, key string) (Environment, error) {
	if err := services.RequireIDOrKey(environmentID, key); err != nil {
		return Environment{}, err
	}

	environment, err := s.get(ctx, environmentID, key)
	if errors.Is(err, sql.ErrNoRows) {
		return Environment{}, fmt.Errorf("%w: environment not found", failure.ErrNotFound)
	}

	if err != nil {
		return Environment{}, fmt.Errorf("get environment: %w", err)
	}

	return s.reconcile(ctx, environment)
}

// SSHConnection returns private SSH connection metadata for an environment.
func (s *Service) SSHConnection(ctx context.Context, environmentID string) (SSHConnection, error) {
	environment, err := s.Get(ctx, environmentID)
	if err != nil {
		return SSHConnection{}, err
	}

	if environment.Status != ch.StateRunning && environment.Status != ch.StatePaused {
		return SSHConnection{}, fmt.Errorf("%w: environment status is %q, want running", failure.ErrFailedDependency, environment.Status)
	}

	record, err := s.getRecord(ctx, environment.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return SSHConnection{}, fmt.Errorf("%w: environment not found", failure.ErrNotFound)
	}

	if err != nil {
		return SSHConnection{}, fmt.Errorf("get environment ssh metadata: %w", err)
	}

	connection := record.SSHConnection
	if connection.Port == 0 {
		connection.Port = ch.SSHPort
	}

	if connection.User == "" {
		connection.User = ch.SSHUser
	}

	if connection.Host == "" || connection.KeyPath == "" {
		return SSHConnection{}, fmt.Errorf("%w: environment does not have SSH connection metadata", failure.ErrFailedDependency)
	}

	return connection, nil
}

// Remove deletes an environment and returns the removed record.
func (s *Service) Remove(ctx context.Context, environmentID string) (Environment, error) {
	environment, err := s.Get(ctx, environmentID)
	if err != nil {
		return Environment{}, err
	}

	return s.remove(ctx, environment)
}

// RemoveByKey deletes an environment by key and returns the removed record.
func (s *Service) RemoveByKey(ctx context.Context, key string) (Environment, error) {
	environment, err := s.GetByKey(ctx, key)
	if err != nil {
		return Environment{}, err
	}

	return s.remove(ctx, environment)
}

func (s *Service) remove(ctx context.Context, environment Environment) (Environment, error) {
	if err := s.updateStatus(ctx, environment.ID, "removing", ""); err != nil {
		return Environment{}, err
	}

	if _, err := s.orchestrator.Remove(ctx, environment.ID); err != nil {
		_ = s.updateStatus(ctx, environment.ID, ch.StateError, err.Error())

		return Environment{}, fmt.Errorf("remove environment vm: %w", err)
	}

	if err := s.deleteVM(ctx, environment.ID); err != nil {
		return Environment{}, err
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM environments WHERE id = ?`, environment.ID); err != nil {
		return Environment{}, fmt.Errorf("remove environment: %w", err)
	}

	return Environment{
		ID:         environment.ID,
		Key:        services.CopyStringPtr(environment.Key),
		Status:     "removed",
		TemplateID: environment.TemplateID,
		Tags:       environment.Tags,
		CreatedAt:  environment.CreatedAt,
		UpdatedAt:  services.Now(),
	}, nil
}

type resolvedTemplate struct {
	ID     string
	Key    *string
	Config json.RawMessage
}

func (s *Service) resolveTemplate(ctx context.Context, templateID, templateKey string) (resolvedTemplate, error) {
	where, value := services.LookupClause(templateID, templateKey, "id", "key")

	var (
		template resolvedTemplate
		key      sql.NullString
		config   string
	)

	err := s.db.QueryRowContext(ctx, `SELECT id, key, config FROM templates WHERE `+where, value).Scan(&template.ID, &key, &config)
	if errors.Is(err, sql.ErrNoRows) {
		return resolvedTemplate{}, fmt.Errorf("%w: template not found", failure.ErrNotFound)
	}

	if err != nil {
		return resolvedTemplate{}, fmt.Errorf("resolve template: %w", err)
	}

	template.Key = services.NullStringPtr(key)
	template.Config = json.RawMessage(config)

	return template, nil
}

func (s *Service) get(ctx context.Context, environmentID, key string) (Environment, error) {
	record, err := s.getRecordByIDOrKey(ctx, environmentID, key)
	if err != nil {
		return Environment{}, err
	}

	entries := []Environment{record.Environment}
	if err := s.loadTags(ctx, entries); err != nil {
		return Environment{}, err
	}

	record.Environment = entries[0]

	return record.Environment, nil
}

func (s *Service) getRecord(ctx context.Context, environmentID string) (environmentRecord, error) {
	return s.getRecordByIDOrKey(ctx, environmentID, "")
}

func (s *Service) getRecordByIDOrKey(ctx context.Context, environmentID, key string) (environmentRecord, error) {
	where, value := services.LookupClause(environmentID, key, "e.id", "e.key")
	row := s.db.QueryRowContext(ctx, environmentSelectQuery()+` WHERE `+where, value)

	return scanEnvironmentRecord(row)
}

func (s *Service) reconcile(ctx context.Context, environment Environment) (Environment, error) {
	if environment.Status == "removing" || environment.Status == "removed" {
		return environment, nil
	}

	vm, err := s.orchestrator.State(ctx, environment.ID)
	if err != nil {
		return environment, fmt.Errorf("reconcile environment vm: %w", err)
	}

	if vm.State == "" {
		return Environment{}, fmt.Errorf("reconcile environment vm: empty runtime state for environment %s", environment.ID)
	}

	if vm.State == ch.StateStopped {
		if err := s.deleteVM(ctx, environment.ID); err != nil {
			return Environment{}, err
		}

		if err := s.updateStatus(ctx, environment.ID, statusFromVM(vm), vm.LastError); err != nil {
			return Environment{}, err
		}

		return s.get(ctx, environment.ID, "")
	}

	if err := s.saveVM(ctx, vm); err != nil {
		return Environment{}, err
	}

	if err := s.updateStatus(ctx, environment.ID, statusFromVM(vm), vm.LastError); err != nil {
		return Environment{}, err
	}

	return s.get(ctx, environment.ID, "")
}

func (s *Service) saveVM(ctx context.Context, vm ch.VM) error {
	if vm.EnvironmentID == "" {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO environment_vms (
  environment_id, vm_id, state, pid, env_dir, runtime_dir, socket_path, kernel_path, rootfs_path,
  tap_name, host_ip, guest_ip, guest_cidr, guest_mac, ssh_user, ssh_port, ssh_key_path,
  created_at, updated_at, last_error
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(environment_id) DO UPDATE SET
  vm_id = excluded.vm_id,
  state = excluded.state,
  pid = excluded.pid,
  env_dir = excluded.env_dir,
  runtime_dir = excluded.runtime_dir,
  socket_path = excluded.socket_path,
  kernel_path = excluded.kernel_path,
  rootfs_path = excluded.rootfs_path,
  tap_name = excluded.tap_name,
  host_ip = excluded.host_ip,
  guest_ip = excluded.guest_ip,
  guest_cidr = excluded.guest_cidr,
  guest_mac = excluded.guest_mac,
  ssh_user = excluded.ssh_user,
  ssh_port = excluded.ssh_port,
  ssh_key_path = excluded.ssh_key_path,
  updated_at = excluded.updated_at,
  last_error = excluded.last_error
`, vm.EnvironmentID, vm.VMID, vm.State, vm.PID, vm.EnvDir, vm.RuntimeDir, vm.SocketPath, vm.KernelPath, vm.RootfsPath, vm.TapName, vm.HostIP, vm.GuestIP, vm.GuestCIDR, vm.GuestMAC, vm.SSHUser, vm.SSHPort, vm.SSHKeyPath, fallbackTime(vm.CreatedAt), fallbackTime(vm.UpdatedAt), vm.LastError)
	if err != nil {
		return fmt.Errorf("save environment vm: %w", err)
	}

	return nil
}

func (s *Service) recordLaunchFailure(ctx context.Context, environmentID string, vm ch.VM, launchErr error) error {
	if vm.EnvironmentID == "" {
		return s.updateStatus(ctx, environmentID, ch.StateError, launchErr.Error())
	}

	if vm.State == "" {
		vm.State = ch.StateError
	}

	if vm.LastError == "" {
		vm.LastError = launchErr.Error()
	}

	return errors.Join(
		s.saveVM(ctx, vm),
		s.updateStatus(ctx, environmentID, statusFromVM(vm), vm.LastError),
	)
}

func (s *Service) deleteVM(ctx context.Context, environmentID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM environment_vms WHERE environment_id = ?`, environmentID)
	if err != nil {
		return fmt.Errorf("delete environment vm: %w", err)
	}

	return nil
}

func (s *Service) updateStatus(ctx context.Context, environmentID, status, lastError string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE environments SET status = ?, updated_at = ?, last_error = ? WHERE id = ?`, status, services.Now(), lastError, environmentID)
	if err != nil {
		return fmt.Errorf("update environment status: %w", err)
	}

	return nil
}

func environmentSelectQuery() string {
	return `
SELECT
  e.id, e.key, e.status, e.template_id, e.created_at, e.updated_at, e.last_error,
  v.guest_ip, v.ssh_port, v.ssh_user, v.ssh_key_path, v.last_error
FROM environments e
LEFT JOIN environment_vms v ON v.environment_id = e.id`
}

type rowScanner interface {
	Scan(...any) error
}

func scanEnvironmentRecord(row rowScanner) (environmentRecord, error) {
	var (
		record      environmentRecord
		key         sql.NullString
		sshHost     sql.NullString
		sshPort     sql.NullInt64
		sshUser     sql.NullString
		sshKeyPath  sql.NullString
		vmLastError sql.NullString
	)

	if err := row.Scan(
		&record.ID,
		&key,
		&record.Status,
		&record.TemplateID,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.LastError,
		&sshHost,
		&sshPort,
		&sshUser,
		&sshKeyPath,
		&vmLastError,
	); err != nil {
		return environmentRecord{}, err
	}

	record.Key = services.NullStringPtr(key)
	record.SSHConnection = SSHConnection{
		Host:    nullString(sshHost),
		Port:    int(sshPort.Int64),
		User:    nullString(sshUser),
		KeyPath: nullString(sshKeyPath),
	}
	record.VMLastError = nullString(vmLastError)

	if record.LastError == "" {
		record.LastError = record.VMLastError
	}

	record.Tags = []string{}

	return record, nil
}

func (s *Service) loadTags(ctx context.Context, entries []Environment) error {
	if len(entries) == 0 {
		return nil
	}

	placeholders := make([]string, len(entries))
	args := make([]any, len(entries))
	indices := make(map[string]int, len(entries))

	for i := range entries {
		placeholders[i] = "?"
		args[i] = entries[i].ID
		indices[entries[i].ID] = i
		entries[i].Tags = []string{}
	}

	rows, err := s.db.QueryContext(ctx, `SELECT environment_id, tag FROM environment_tags WHERE environment_id IN (`+strings.Join(placeholders, ",")+`) ORDER BY environment_id, position`, args...)
	if err != nil {
		return fmt.Errorf("list environment tags: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var environmentID, tag string
		if err := rows.Scan(&environmentID, &tag); err != nil {
			return fmt.Errorf("scan environment tag: %w", err)
		}

		if index, ok := indices[environmentID]; ok {
			entries[index].Tags = append(entries[index].Tags, tag)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate environment tags: %w", err)
	}

	return nil
}

func validateTags(tags []string) error {
	for _, tag := range tags {
		if strings.TrimSpace(tag) == "" {
			return fmt.Errorf("%w: environment tag is required", failure.ErrInvalid)
		}
	}

	return nil
}

func uniqueTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}

	unique := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))

	for _, tag := range tags {
		if _, ok := seen[tag]; ok {
			continue
		}

		seen[tag] = struct{}{}
		unique = append(unique, tag)
	}

	return unique
}

func copyTags(tags []string) []string {
	if len(tags) == 0 {
		return []string{}
	}

	return append([]string(nil), tags...)
}

func nullString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}

	return value.String
}

func statusFromVM(vm ch.VM) string {
	if vm.State == "" {
		return ch.StateRunning
	}

	return vm.State
}

func fallbackTime(value string) string {
	if value == "" {
		return services.Now()
	}

	return value
}

type noopOrchestrator struct{}

func (noopOrchestrator) Launch(_ context.Context, req ch.LaunchRequest) (ch.VM, error) {
	now := services.Now()

	return ch.VM{
		EnvironmentID: req.EnvironmentID,
		VMID:          req.EnvironmentID,
		State:         ch.StateRunning,
		SSHUser:       ch.SSHUser,
		SSHPort:       ch.SSHPort,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func (noopOrchestrator) State(_ context.Context, environmentID string) (ch.VM, error) {
	return ch.VM{EnvironmentID: environmentID, State: ch.StateRunning, SSHUser: ch.SSHUser, SSHPort: ch.SSHPort, UpdatedAt: services.Now()}, nil
}

func (noopOrchestrator) Remove(_ context.Context, environmentID string) (ch.VM, error) {
	return ch.VM{EnvironmentID: environmentID, State: ch.StateStopped, UpdatedAt: services.Now()}, nil
}
