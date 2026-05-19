// Package environment manages Bastion environment records.
package environment

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/failure"
	fc "github.com/bastion-computer/bastion/core/internal/firecracker"
	"github.com/bastion-computer/bastion/core/internal/services"
)

// Environment describes a managed opencode environment.
type Environment struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	TemplateID   string `json:"templateId"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
	LastError    string `json:"lastError,omitempty"`
	VMID         string `json:"vmId,omitempty"`
	SSHHost      string `json:"sshHost,omitempty"`
	SSHPort      int    `json:"sshPort,omitempty"`
	SSHUser      string `json:"sshUser,omitempty"`
	SSHKeyPath   string `json:"sshKeyPath,omitempty"`
	RuntimeState string `json:"runtimeState,omitempty"`
}

// CreateRequest contains the fields needed to create an environment.
type CreateRequest struct {
	TemplateID  string `json:"templateId,omitempty"`
	TemplateKey string `json:"templateKey,omitempty"`
}

// Orchestrator launches, reconciles, and removes environment VMs.
type Orchestrator interface {
	Launch(context.Context, fc.LaunchRequest) (fc.VM, error)
	State(context.Context, string) (fc.VM, error)
	Remove(context.Context, string) (fc.VM, error)
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

	template, err := s.resolveTemplate(ctx, req.TemplateID, req.TemplateKey)
	if err != nil {
		return Environment{}, err
	}

	environmentID, err := services.GenerateID("env")
	if err != nil {
		return Environment{}, err
	}

	now := services.Now()
	environment := Environment{ID: environmentID, Status: fc.StateCreating, TemplateID: template.ID, CreatedAt: now, UpdatedAt: now}

	_, err = s.db.ExecContext(ctx, `INSERT INTO environments (id, status, template_id, created_at, updated_at, last_error) VALUES (?, ?, ?, ?, ?, ?)`, environment.ID, environment.Status, environment.TemplateID, environment.CreatedAt, environment.UpdatedAt, "")
	if err != nil {
		if database.IsConstraint(err) {
			return Environment{}, fmt.Errorf("%w: environment already exists", failure.ErrConflict)
		}

		return Environment{}, fmt.Errorf("create environment: %w", err)
	}

	vm, err := s.orchestrator.Launch(ctx, fc.LaunchRequest{
		EnvironmentID: environment.ID,
		Template: fc.Template{
			ID:     template.ID,
			Key:    template.Key,
			Config: template.Config,
		},
	})
	if err != nil {
		_ = s.updateStatus(ctx, environment.ID, fc.StateError, err.Error())

		return Environment{}, fmt.Errorf("launch environment vm: %w", err)
	}

	if err := s.saveVM(ctx, vm); err != nil {
		_, _ = s.orchestrator.Remove(context.Background(), environment.ID)
		_ = s.updateStatus(ctx, environment.ID, fc.StateError, err.Error())

		return Environment{}, err
	}

	if err := s.updateStatus(ctx, environment.ID, statusFromVM(vm), vm.LastError); err != nil {
		_, _ = s.orchestrator.Remove(context.Background(), environment.ID)

		return Environment{}, err
	}

	return s.Get(ctx, environment.ID)
}

// List returns environments ordered by creation time.
func (s *Service) List(ctx context.Context, limit int, cursor string) (services.Page[Environment], error) {
	limit = services.NormalizeLimit(limit)

	query := environmentSelectQuery()

	var (
		rows *sql.Rows
		err  error
	)

	if cursor == "" {
		rows, err = s.db.QueryContext(ctx, query+` ORDER BY e.created_at LIMIT ?`, limit+1)
	} else {
		rows, err = s.db.QueryContext(ctx, query+` WHERE e.created_at > ? ORDER BY e.created_at LIMIT ?`, cursor, limit+1)
	}

	if err != nil {
		return services.Page[Environment]{}, fmt.Errorf("list environments: %w", err)
	}

	defer func() { _ = rows.Close() }()

	entries := make([]Environment, 0, limit+1)

	for rows.Next() {
		environment, err := scanEnvironment(rows)
		if err != nil {
			return services.Page[Environment]{}, fmt.Errorf("scan environment: %w", err)
		}

		entries = append(entries, environment)
	}

	if err := rows.Err(); err != nil {
		return services.Page[Environment]{}, fmt.Errorf("iterate environments: %w", err)
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
	environment, err := s.get(ctx, environmentID)
	if errors.Is(err, sql.ErrNoRows) {
		return Environment{}, fmt.Errorf("%w: environment not found", failure.ErrNotFound)
	}

	if err != nil {
		return Environment{}, fmt.Errorf("get environment: %w", err)
	}

	return s.reconcile(ctx, environment)
}

// Remove deletes an environment and returns the removed record.
func (s *Service) Remove(ctx context.Context, environmentID string) (Environment, error) {
	environment, err := s.Get(ctx, environmentID)
	if err != nil {
		return Environment{}, err
	}

	if err := s.updateStatus(ctx, environment.ID, "removing", ""); err != nil {
		return Environment{}, err
	}

	if _, err := s.orchestrator.Remove(ctx, environment.ID); err != nil {
		_ = s.updateStatus(ctx, environment.ID, fc.StateError, err.Error())

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
		Status:     "removed",
		TemplateID: environment.TemplateID,
		CreatedAt:  environment.CreatedAt,
		UpdatedAt:  services.Now(),
	}, nil
}

type resolvedTemplate struct {
	ID     string
	Key    string
	Config json.RawMessage
}

func (s *Service) resolveTemplate(ctx context.Context, templateID, templateKey string) (resolvedTemplate, error) {
	where, value := services.LookupClause(templateID, templateKey, "id", "key")

	var (
		template resolvedTemplate
		config   string
	)

	err := s.db.QueryRowContext(ctx, `SELECT id, key, config FROM templates WHERE `+where, value).Scan(&template.ID, &template.Key, &config)
	if errors.Is(err, sql.ErrNoRows) {
		return resolvedTemplate{}, fmt.Errorf("%w: template not found", failure.ErrNotFound)
	}

	if err != nil {
		return resolvedTemplate{}, fmt.Errorf("resolve template: %w", err)
	}

	template.Config = json.RawMessage(config)

	return template, nil
}

func (s *Service) get(ctx context.Context, environmentID string) (Environment, error) {
	row := s.db.QueryRowContext(ctx, environmentSelectQuery()+` WHERE e.id = ?`, environmentID)

	return scanEnvironment(row)
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
		return environment, nil
	}

	if vm.State == fc.StateStopped {
		if err := s.deleteVM(ctx, environment.ID); err != nil {
			return Environment{}, err
		}
	} else if err := s.saveVM(ctx, vm); err != nil {
		return Environment{}, err
	}

	if err := s.updateStatus(ctx, environment.ID, statusFromVM(vm), vm.LastError); err != nil {
		return Environment{}, err
	}

	return s.get(ctx, environment.ID)
}

func (s *Service) saveVM(ctx context.Context, vm fc.VM) error {
	if vm.EnvironmentID == "" {
		return nil
	}

	_, err := s.db.ExecContext(ctx, `
INSERT INTO environment_vms (
  environment_id, vm_id, state, pid, env_dir, jailer_dir, socket_path, kernel_path, rootfs_path,
  tap_name, host_ip, guest_ip, guest_cidr, guest_mac, ssh_user, ssh_port, ssh_key_path,
  created_at, updated_at, last_error
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(environment_id) DO UPDATE SET
  vm_id = excluded.vm_id,
  state = excluded.state,
  pid = excluded.pid,
  env_dir = excluded.env_dir,
  jailer_dir = excluded.jailer_dir,
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
`, vm.EnvironmentID, vm.VMID, vm.State, vm.PID, vm.EnvDir, vm.JailerDir, vm.SocketPath, vm.KernelPath, vm.RootfsPath, vm.TapName, vm.HostIP, vm.GuestIP, vm.GuestCIDR, vm.GuestMAC, vm.SSHUser, vm.SSHPort, vm.SSHKeyPath, fallbackTime(vm.CreatedAt), fallbackTime(vm.UpdatedAt), vm.LastError)
	if err != nil {
		return fmt.Errorf("save environment vm: %w", err)
	}

	return nil
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
  e.id, e.status, e.template_id, e.created_at, e.updated_at, e.last_error,
  v.vm_id, v.state, v.guest_ip, v.ssh_port, v.ssh_user, v.ssh_key_path, v.last_error
FROM environments e
LEFT JOIN environment_vms v ON v.environment_id = e.id`
}

type rowScanner interface {
	Scan(...any) error
}

func scanEnvironment(row rowScanner) (Environment, error) {
	var (
		environment  Environment
		vmID         sql.NullString
		runtimeState sql.NullString
		sshHost      sql.NullString
		sshPort      sql.NullInt64
		sshUser      sql.NullString
		sshKeyPath   sql.NullString
		vmLastError  sql.NullString
	)

	if err := row.Scan(
		&environment.ID,
		&environment.Status,
		&environment.TemplateID,
		&environment.CreatedAt,
		&environment.UpdatedAt,
		&environment.LastError,
		&vmID,
		&runtimeState,
		&sshHost,
		&sshPort,
		&sshUser,
		&sshKeyPath,
		&vmLastError,
	); err != nil {
		return Environment{}, err
	}

	environment.VMID = nullString(vmID)
	environment.RuntimeState = nullString(runtimeState)
	environment.SSHHost = nullString(sshHost)
	environment.SSHPort = int(sshPort.Int64)
	environment.SSHUser = nullString(sshUser)
	environment.SSHKeyPath = nullString(sshKeyPath)

	if environment.LastError == "" {
		environment.LastError = nullString(vmLastError)
	}

	return environment, nil
}

func nullString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}

	return value.String
}

func statusFromVM(vm fc.VM) string {
	if vm.State == "" {
		return fc.StateRunning
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

func (noopOrchestrator) Launch(_ context.Context, req fc.LaunchRequest) (fc.VM, error) {
	now := services.Now()

	return fc.VM{
		EnvironmentID: req.EnvironmentID,
		VMID:          req.EnvironmentID,
		State:         fc.StateRunning,
		SSHUser:       fc.SSHUser,
		SSHPort:       fc.SSHPort,
		CreatedAt:     now,
		UpdatedAt:     now,
	}, nil
}

func (noopOrchestrator) State(_ context.Context, environmentID string) (fc.VM, error) {
	return fc.VM{EnvironmentID: environmentID, State: fc.StateRunning, SSHUser: fc.SSHUser, SSHPort: fc.SSHPort, UpdatedAt: services.Now()}, nil
}

func (noopOrchestrator) Remove(_ context.Context, environmentID string) (fc.VM, error) {
	return fc.VM{EnvironmentID: environmentID, State: fc.StateStopped, UpdatedAt: services.Now()}, nil
}
