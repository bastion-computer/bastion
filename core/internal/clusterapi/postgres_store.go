//nolint:dupl,wsl_v5 // Postgres store CRUD methods intentionally mirror each resource table.
package clusterapi

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/cluster"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/secret"
	"github.com/bastion-computer/bastion/core/internal/services/template"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// PostgresStore persists cluster state in Postgres.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// OpenPostgresStore connects to Postgres and applies cluster migrations.
func OpenPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, errors.New("cluster database URL is required")
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open cluster postgres database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()

		return nil, fmt.Errorf("connect cluster postgres database: %w", err)
	}

	store := &PostgresStore{pool: pool}
	if err := store.runMigrations(ctx); err != nil {
		pool.Close()

		return nil, err
	}

	return store, nil
}

// Close releases Postgres connections.
func (s *PostgresStore) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *PostgresStore) runMigrations(ctx context.Context) error {
	if _, err := s.pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS cluster_schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create cluster migration table: %w", err)
	}

	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read cluster migrations: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		version := strings.TrimSuffix(name, filepath.Ext(name))
		applied, err := s.migrationApplied(ctx, version)
		if err != nil {
			return err
		}

		if applied {
			continue
		}

		contents, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read cluster migration %s: %w", name, err)
		}

		if err := s.applyMigration(ctx, version, string(contents)); err != nil {
			return err
		}
	}

	return nil
}

func (s *PostgresStore) migrationApplied(ctx context.Context, version string) (bool, error) {
	var found string
	err := s.pool.QueryRow(ctx, `SELECT version FROM cluster_schema_migrations WHERE version = $1`, version).Scan(&found)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}

	if err != nil {
		return false, fmt.Errorf("query cluster migration %s: %w", version, err)
	}

	return true, nil
}

func (s *PostgresStore) applyMigration(ctx context.Context, version, sql string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin cluster migration %s: %w", version, err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err := tx.Exec(ctx, sql); err != nil {
		return fmt.Errorf("apply cluster migration %s: %w", version, err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO cluster_schema_migrations (version, applied_at) VALUES ($1, $2)`, version, services.Now()); err != nil {
		return fmt.Errorf("record cluster migration %s: %w", version, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cluster migration %s: %w", version, err)
	}

	committed = true

	return nil
}

// CreateNode stores a cluster node.
func (s *PostgresStore) CreateNode(ctx context.Context, req cluster.CreateNodeRequest) (cluster.Node, error) {
	if err := validateNodeRequest(req); err != nil {
		return cluster.Node{}, err
	}

	id, err := services.GenerateID(nodeIDPrefix)
	if err != nil {
		return cluster.Node{}, err
	}

	node := cluster.Node{ID: id, Key: services.CopyStringPtr(req.Key), APIURL: strings.TrimRight(req.APIURL, "/"), CreatedAt: services.Now()}
	_, err = s.pool.Exec(ctx, `INSERT INTO cluster_nodes (id, key, api_url, created_at) VALUES ($1, $2, $3, $4)`, node.ID, optionalString(node.Key), node.APIURL, node.CreatedAt)
	if err != nil {
		return cluster.Node{}, postgresCreateError(err, "node")
	}

	return node, nil
}

// ListNodes returns registered nodes ordered by creation time.
func (s *PostgresStore) ListNodes(ctx context.Context, limit int, cursor string) (services.Page[cluster.Node], error) {
	limit = services.NormalizeLimit(limit)
	rows, err := s.pool.Query(ctx, `SELECT id, key, api_url, created_at FROM cluster_nodes WHERE ($1 = '' OR created_at > $1) ORDER BY created_at LIMIT $2`, cursor, limit+1)
	if err != nil {
		return services.Page[cluster.Node]{}, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()

	entries := make([]cluster.Node, 0, limit+1)
	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return services.Page[cluster.Node]{}, fmt.Errorf("scan node: %w", err)
		}

		entries = append(entries, node)
	}

	if err := rows.Err(); err != nil {
		return services.Page[cluster.Node]{}, fmt.Errorf("iterate nodes: %w", err)
	}

	return services.FromEntries(entries, limit, func(node cluster.Node) string { return node.CreatedAt }), nil
}

// GetNode returns a node by ID or key.
func (s *PostgresStore) GetNode(ctx context.Context, id, key string) (cluster.Node, error) {
	if err := services.RequireIDOrKey(id, key); err != nil {
		return cluster.Node{}, err
	}

	where, value := postgresLookupClause(id, key, "id", "key")
	node, err := scanNode(s.pool.QueryRow(ctx, `SELECT id, key, api_url, created_at FROM cluster_nodes WHERE `+where, value))
	if errors.Is(err, pgx.ErrNoRows) {
		return cluster.Node{}, fmt.Errorf("%w: node not found", failure.ErrNotFound)
	}

	if err != nil {
		return cluster.Node{}, fmt.Errorf("get node: %w", err)
	}

	return node, nil
}

// RemoveNode removes a node by ID or key.
func (s *PostgresStore) RemoveNode(ctx context.Context, id, key string) (cluster.Node, error) {
	node, err := s.GetNode(ctx, id, key)
	if err != nil {
		return cluster.Node{}, err
	}

	if _, err := s.pool.Exec(ctx, `DELETE FROM cluster_nodes WHERE id = $1`, node.ID); err != nil {
		return cluster.Node{}, postgresDeleteError(err, "node")
	}

	return node, nil
}

// CreateNamespace stores a tenant namespace.
func (s *PostgresStore) CreateNamespace(ctx context.Context, req cluster.CreateNamespaceRequest) (cluster.Namespace, error) {
	if err := validateNamespaceRequest(req); err != nil {
		return cluster.Namespace{}, err
	}

	id, err := services.GenerateID(namespaceIDPrefix)
	if err != nil {
		return cluster.Namespace{}, err
	}

	namespace := cluster.Namespace{ID: id, Key: services.CopyStringPtr(req.Key), Limits: req.Limits, CreatedAt: services.Now()}
	_, err = s.pool.Exec(ctx, `INSERT INTO cluster_namespaces (id, key, vcpu_limit, memory_limit, volume_limit, created_at) VALUES ($1, $2, $3, $4, $5, $6)`, namespace.ID, optionalString(namespace.Key), namespace.Limits.VCPU, namespace.Limits.MemoryBytes, namespace.Limits.VolumeBytes, namespace.CreatedAt)
	if err != nil {
		return cluster.Namespace{}, postgresCreateError(err, "namespace")
	}

	return namespace, nil
}

// ListNamespaces returns namespaces ordered by creation time.
func (s *PostgresStore) ListNamespaces(ctx context.Context, limit int, cursor string) (services.Page[cluster.Namespace], error) {
	limit = services.NormalizeLimit(limit)
	rows, err := s.pool.Query(ctx, `SELECT id, key, vcpu_limit, memory_limit, volume_limit, created_at FROM cluster_namespaces WHERE ($1 = '' OR created_at > $1) ORDER BY created_at LIMIT $2`, cursor, limit+1)
	if err != nil {
		return services.Page[cluster.Namespace]{}, fmt.Errorf("list namespaces: %w", err)
	}
	defer rows.Close()

	entries := make([]cluster.Namespace, 0, limit+1)
	for rows.Next() {
		namespace, err := scanNamespace(rows)
		if err != nil {
			return services.Page[cluster.Namespace]{}, fmt.Errorf("scan namespace: %w", err)
		}

		entries = append(entries, namespace)
	}

	if err := rows.Err(); err != nil {
		return services.Page[cluster.Namespace]{}, fmt.Errorf("iterate namespaces: %w", err)
	}

	return services.FromEntries(entries, limit, func(namespace cluster.Namespace) string { return namespace.CreatedAt }), nil
}

// GetNamespace returns a namespace by ID or key.
func (s *PostgresStore) GetNamespace(ctx context.Context, id, key string) (cluster.Namespace, error) {
	if err := services.RequireIDOrKey(id, key); err != nil {
		return cluster.Namespace{}, err
	}

	where, value := postgresLookupClause(id, key, "id", "key")
	namespace, err := scanNamespace(s.pool.QueryRow(ctx, `SELECT id, key, vcpu_limit, memory_limit, volume_limit, created_at FROM cluster_namespaces WHERE `+where, value))
	if errors.Is(err, pgx.ErrNoRows) {
		return cluster.Namespace{}, fmt.Errorf("%w: namespace not found", failure.ErrNotFound)
	}

	if err != nil {
		return cluster.Namespace{}, fmt.Errorf("get namespace: %w", err)
	}

	return namespace, nil
}

// RemoveNamespace removes a namespace by ID or key.
func (s *PostgresStore) RemoveNamespace(ctx context.Context, id, key string) (cluster.Namespace, error) {
	namespace, err := s.GetNamespace(ctx, id, key)
	if err != nil {
		return cluster.Namespace{}, err
	}

	if _, err := s.pool.Exec(ctx, `DELETE FROM cluster_namespaces WHERE id = $1`, namespace.ID); err != nil {
		return cluster.Namespace{}, postgresDeleteError(err, "namespace")
	}

	return namespace, nil
}

// ResolveNamespace resolves ns_ identifiers by ID and all other values by key.
func (s *PostgresStore) ResolveNamespace(ctx context.Context, reference string) (cluster.Namespace, error) {
	if strings.HasPrefix(reference, namespaceIDPrefix+"_") {
		return s.GetNamespace(ctx, reference, "")
	}

	return s.GetNamespace(ctx, "", reference)
}

// CreateSecret stores a source secret inside a namespace.
func (s *PostgresStore) CreateSecret(ctx context.Context, namespaceID string, req secret.CreateRequest) (secret.Metadata, error) {
	if err := validateSecretRequest(req); err != nil {
		return secret.Metadata{}, err
	}

	id, err := services.GenerateID("sec")
	if err != nil {
		return secret.Metadata{}, err
	}

	created := secret.Secret{ID: id, Key: services.CopyStringPtr(req.Key), Value: req.Value, CreatedAt: services.Now()}
	_, err = s.pool.Exec(ctx, `INSERT INTO cluster_secrets (namespace_id, id, key, value, created_at) VALUES ($1, $2, $3, $4, $5)`, namespaceID, created.ID, optionalString(created.Key), created.Value, created.CreatedAt)
	if err != nil {
		return secret.Metadata{}, postgresCreateError(err, "secret")
	}

	return created.Metadata(), nil
}

// ListSecrets returns source secret metadata in a namespace.
func (s *PostgresStore) ListSecrets(ctx context.Context, namespaceID string, limit int, cursor string) (services.Page[secret.Metadata], error) {
	limit = services.NormalizeLimit(limit)
	rows, err := s.pool.Query(ctx, `SELECT id, key, created_at FROM cluster_secrets WHERE namespace_id = $1 AND ($2 = '' OR created_at > $2) ORDER BY created_at LIMIT $3`, namespaceID, cursor, limit+1)
	if err != nil {
		return services.Page[secret.Metadata]{}, fmt.Errorf("list secrets: %w", err)
	}
	defer rows.Close()

	entries := make([]secret.Metadata, 0, limit+1)
	for rows.Next() {
		var entry secret.Metadata
		var key *string
		if err := rows.Scan(&entry.ID, &key, &entry.CreatedAt); err != nil {
			return services.Page[secret.Metadata]{}, fmt.Errorf("scan secret: %w", err)
		}

		entry.Key = services.CopyStringPtr(key)
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return services.Page[secret.Metadata]{}, fmt.Errorf("iterate secrets: %w", err)
	}

	return services.FromEntries(entries, limit, func(entry secret.Metadata) string { return entry.CreatedAt }), nil
}

// GetSecret returns a source secret by ID or key.
func (s *PostgresStore) GetSecret(ctx context.Context, namespaceID, id, key string) (secret.Secret, error) {
	if err := services.RequireIDOrKey(id, key); err != nil {
		return secret.Secret{}, err
	}

	where, value := postgresLookupClause(id, key, "id", "key")
	stored, err := scanSecret(s.pool.QueryRow(ctx, `SELECT id, key, value, created_at FROM cluster_secrets WHERE namespace_id = $1 AND `+strings.Replace(where, "$1", "$2", 1), namespaceID, value))
	if errors.Is(err, pgx.ErrNoRows) {
		return secret.Secret{}, fmt.Errorf("%w: secret not found", failure.ErrNotFound)
	}

	if err != nil {
		return secret.Secret{}, fmt.Errorf("get secret: %w", err)
	}

	return stored, nil
}

// RemoveSecret deletes a source secret by ID or key.
func (s *PostgresStore) RemoveSecret(ctx context.Context, namespaceID, id, key string) (secret.Metadata, error) {
	stored, err := s.GetSecret(ctx, namespaceID, id, key)
	if err != nil {
		return secret.Metadata{}, err
	}

	if _, err := s.pool.Exec(ctx, `DELETE FROM cluster_secrets WHERE namespace_id = $1 AND id = $2`, namespaceID, stored.ID); err != nil {
		return secret.Metadata{}, postgresDeleteError(err, "secret")
	}

	return stored.Metadata(), nil
}

// CreateTemplate stores a source template.
func (s *PostgresStore) CreateTemplate(ctx context.Context, namespaceID string, source template.Template, archiveKey string) (template.Metadata, error) {
	if err := validateStoredTemplate(source, archiveKey); err != nil {
		return template.Metadata{}, err
	}

	_, err := s.pool.Exec(ctx, `INSERT INTO cluster_templates (namespace_id, id, key, config, archive_key, created_at) VALUES ($1, $2, $3, $4, $5, $6)`, namespaceID, source.ID, optionalString(source.Key), string(source.Config), archiveKey, source.CreatedAt)
	if err != nil {
		return template.Metadata{}, postgresCreateError(err, "template")
	}

	return source.Metadata(), nil
}

// ListTemplates returns source template metadata in a namespace.
func (s *PostgresStore) ListTemplates(ctx context.Context, namespaceID string, limit int, cursor string) (services.Page[template.Metadata], error) {
	limit = services.NormalizeLimit(limit)
	rows, err := s.pool.Query(ctx, `SELECT id, key, created_at FROM cluster_templates WHERE namespace_id = $1 AND ($2 = '' OR created_at > $2) ORDER BY created_at LIMIT $3`, namespaceID, cursor, limit+1)
	if err != nil {
		return services.Page[template.Metadata]{}, fmt.Errorf("list templates: %w", err)
	}
	defer rows.Close()

	entries := make([]template.Metadata, 0, limit+1)
	for rows.Next() {
		var entry template.Metadata
		var key *string
		if err := rows.Scan(&entry.ID, &key, &entry.CreatedAt); err != nil {
			return services.Page[template.Metadata]{}, fmt.Errorf("scan template: %w", err)
		}

		entry.Key = services.CopyStringPtr(key)
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return services.Page[template.Metadata]{}, fmt.Errorf("iterate templates: %w", err)
	}

	return services.FromEntries(entries, limit, func(entry template.Metadata) string { return entry.CreatedAt }), nil
}

// GetTemplate returns a source template by ID or key.
func (s *PostgresStore) GetTemplate(ctx context.Context, namespaceID, id, key string) (StoredTemplate, error) {
	if err := services.RequireIDOrKey(id, key); err != nil {
		return StoredTemplate{}, err
	}

	where, value := postgresLookupClause(id, key, "id", "key")
	stored, err := scanStoredTemplate(s.pool.QueryRow(ctx, `SELECT id, key, config, archive_key, created_at FROM cluster_templates WHERE namespace_id = $1 AND `+strings.Replace(where, "$1", "$2", 1), namespaceID, value))
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredTemplate{}, fmt.Errorf("%w: template not found", failure.ErrNotFound)
	}

	if err != nil {
		return StoredTemplate{}, fmt.Errorf("get template: %w", err)
	}

	return stored, nil
}

// RemoveTemplate removes a source template if no source environments use it.
func (s *PostgresStore) RemoveTemplate(ctx context.Context, namespaceID, id, key string) (StoredTemplate, error) {
	stored, err := s.GetTemplate(ctx, namespaceID, id, key)
	if err != nil {
		return StoredTemplate{}, err
	}

	if _, err := s.pool.Exec(ctx, `DELETE FROM cluster_templates WHERE namespace_id = $1 AND id = $2`, namespaceID, stored.ID); err != nil {
		return StoredTemplate{}, postgresDeleteError(err, "template")
	}

	return stored, nil
}

// TemplateDerivative returns a node-local derivative template ID.
func (s *PostgresStore) TemplateDerivative(ctx context.Context, sourceTemplateID, nodeID string) (string, bool, error) {
	var derivativeID string
	err := s.pool.QueryRow(ctx, `SELECT derivative_template_id FROM cluster_template_derivatives WHERE source_template_id = $1 AND node_id = $2`, sourceTemplateID, nodeID).Scan(&derivativeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}

	if err != nil {
		return "", false, fmt.Errorf("get template derivative: %w", err)
	}

	return derivativeID, true, nil
}

// SaveTemplateDerivative records a node-local derivative template ID.
func (s *PostgresStore) SaveTemplateDerivative(ctx context.Context, sourceTemplateID, nodeID, derivativeTemplateID string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO cluster_template_derivatives (source_template_id, node_id, derivative_template_id, created_at) VALUES ($1, $2, $3, $4) ON CONFLICT (source_template_id, node_id) DO UPDATE SET derivative_template_id = excluded.derivative_template_id`, sourceTemplateID, nodeID, derivativeTemplateID, services.Now())
	if err != nil {
		return fmt.Errorf("save template derivative: %w", err)
	}

	return nil
}

// RemoveTemplateDerivative removes a node-local derivative template mapping.
func (s *PostgresStore) RemoveTemplateDerivative(ctx context.Context, sourceTemplateID, nodeID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM cluster_template_derivatives WHERE source_template_id = $1 AND node_id = $2`, sourceTemplateID, nodeID)
	if err != nil {
		return fmt.Errorf("remove template derivative: %w", err)
	}

	return nil
}

// CreateEnvironment stores a source environment placement.
func (s *PostgresStore) CreateEnvironment(ctx context.Context, namespaceID string, record EnvironmentRecord) (environment.Environment, error) {
	if err := validateEnvironmentRecord(record); err != nil {
		return environment.Environment{}, err
	}

	tags, err := json.Marshal(record.Tags)
	if err != nil {
		return environment.Environment{}, fmt.Errorf("encode environment tags: %w", err)
	}

	_, err = s.pool.Exec(ctx, `INSERT INTO cluster_environments (namespace_id, id, key, status, source_template_id, node_id, derivative_template_id, derivative_environment_id, tags, created_at, updated_at, last_error) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`, namespaceID, record.ID, optionalString(record.Key), record.Status, record.TemplateID, record.NodeID, record.DerivativeTemplateID, record.DerivativeEnvironmentID, string(tags), record.CreatedAt, record.UpdatedAt, record.LastError)
	if err != nil {
		return environment.Environment{}, postgresCreateError(err, "environment")
	}

	return record.Environment, nil
}

// ListEnvironments returns source environments in a namespace.
func (s *PostgresStore) ListEnvironments(ctx context.Context, namespaceID string, limit int, cursor string, tags []string) (services.Page[environment.Environment], error) {
	limit = services.NormalizeLimit(limit)
	rows, err := s.pool.Query(ctx, `SELECT id, key, status, source_template_id, node_id, derivative_template_id, derivative_environment_id, tags, created_at, updated_at, last_error FROM cluster_environments WHERE namespace_id = $1 AND ($2 = '' OR created_at > $2) ORDER BY created_at`, namespaceID, cursor)
	if err != nil {
		return services.Page[environment.Environment]{}, fmt.Errorf("list environments: %w", err)
	}
	defer rows.Close()

	entries := make([]environment.Environment, 0, limit+1)
	for rows.Next() {
		record, err := scanEnvironmentRecord(rows)
		if err != nil {
			return services.Page[environment.Environment]{}, fmt.Errorf("scan environment: %w", err)
		}

		if environmentHasTags(record.Environment, tags) {
			entries = append(entries, record.Environment)
			if len(entries) > limit {
				break
			}
		}
	}

	if err := rows.Err(); err != nil {
		return services.Page[environment.Environment]{}, fmt.Errorf("iterate environments: %w", err)
	}

	return services.FromEntries(entries, limit, func(entry environment.Environment) string { return entry.CreatedAt }), nil
}

// GetEnvironment returns a source environment by ID or key.
func (s *PostgresStore) GetEnvironment(ctx context.Context, namespaceID, id, key string) (EnvironmentRecord, error) {
	if err := services.RequireIDOrKey(id, key); err != nil {
		return EnvironmentRecord{}, err
	}

	where, value := postgresLookupClause(id, key, "id", "key")
	record, err := scanEnvironmentRecord(s.pool.QueryRow(ctx, `SELECT id, key, status, source_template_id, node_id, derivative_template_id, derivative_environment_id, tags, created_at, updated_at, last_error FROM cluster_environments WHERE namespace_id = $1 AND `+strings.Replace(where, "$1", "$2", 1), namespaceID, value))
	if errors.Is(err, pgx.ErrNoRows) {
		return EnvironmentRecord{}, fmt.Errorf("%w: environment not found", failure.ErrNotFound)
	}

	if err != nil {
		return EnvironmentRecord{}, fmt.Errorf("get environment: %w", err)
	}

	return record, nil
}

// RemoveEnvironment removes a source environment by ID or key.
func (s *PostgresStore) RemoveEnvironment(ctx context.Context, namespaceID, id, key string) (EnvironmentRecord, error) {
	record, err := s.GetEnvironment(ctx, namespaceID, id, key)
	if err != nil {
		return EnvironmentRecord{}, err
	}

	if _, err := s.pool.Exec(ctx, `DELETE FROM cluster_environments WHERE namespace_id = $1 AND id = $2`, namespaceID, record.ID); err != nil {
		return EnvironmentRecord{}, postgresDeleteError(err, "environment")
	}

	return record, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanNode(row rowScanner) (cluster.Node, error) {
	var (
		node cluster.Node
		key  *string
	)
	if err := row.Scan(&node.ID, &key, &node.APIURL, &node.CreatedAt); err != nil {
		return cluster.Node{}, err
	}

	node.Key = services.CopyStringPtr(key)

	return node, nil
}

func scanNamespace(row rowScanner) (cluster.Namespace, error) {
	var (
		namespace cluster.Namespace
		key       *string
	)
	if err := row.Scan(&namespace.ID, &key, &namespace.Limits.VCPU, &namespace.Limits.MemoryBytes, &namespace.Limits.VolumeBytes, &namespace.CreatedAt); err != nil {
		return cluster.Namespace{}, err
	}

	namespace.Key = services.CopyStringPtr(key)

	return namespace, nil
}

func scanSecret(row rowScanner) (secret.Secret, error) {
	var (
		stored secret.Secret
		key    *string
	)
	if err := row.Scan(&stored.ID, &key, &stored.Value, &stored.CreatedAt); err != nil {
		return secret.Secret{}, err
	}

	stored.Key = services.CopyStringPtr(key)

	return stored, nil
}

func scanStoredTemplate(row rowScanner) (StoredTemplate, error) {
	var (
		stored     StoredTemplate
		key        *string
		configText string
	)
	if err := row.Scan(&stored.ID, &key, &configText, &stored.ArchiveKey, &stored.CreatedAt); err != nil {
		return StoredTemplate{}, err
	}

	stored.Key = services.CopyStringPtr(key)
	stored.Config = json.RawMessage(configText)

	return stored, nil
}

func scanEnvironmentRecord(row rowScanner) (EnvironmentRecord, error) {
	var (
		record EnvironmentRecord
		key    *string
		tags   string
	)
	if err := row.Scan(&record.ID, &key, &record.Status, &record.TemplateID, &record.NodeID, &record.DerivativeTemplateID, &record.DerivativeEnvironmentID, &tags, &record.CreatedAt, &record.UpdatedAt, &record.LastError); err != nil {
		return EnvironmentRecord{}, err
	}

	record.Key = services.CopyStringPtr(key)
	if tags == "" {
		record.Tags = []string{}
	} else if err := json.Unmarshal([]byte(tags), &record.Tags); err != nil {
		return EnvironmentRecord{}, err
	}

	return record, nil
}

func optionalString(value *string) any {
	if value == nil {
		return nil
	}

	return *value
}

func postgresLookupClause(id, key, idColumn, keyColumn string) (string, any) {
	if id != "" {
		return idColumn + " = $1", id
	}

	return keyColumn + " = $1", key
}

func postgresCreateError(err error, resource string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return fmt.Errorf("%w: %s already exists", failure.ErrConflict, resource)
	}

	return fmt.Errorf("create %s: %w", resource, err)
}

func postgresDeleteError(err error, resource string) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return fmt.Errorf("%w: %s is in use", failure.ErrConflict, resource)
	}

	return fmt.Errorf("remove %s: %w", resource, err)
}
