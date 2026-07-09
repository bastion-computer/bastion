//nolint:wsl_v5 // Cluster orchestration intentionally keeps related DB/error handling steps adjacent.
package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
	"github.com/bastion-computer/bastion/core/internal/clusterdb"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/template"
	"github.com/bastion-computer/bastion/core/internal/services/utilization"
	"github.com/bastion-computer/bastion/core/internal/templatearchive"
	"github.com/bastion-computer/bastion/core/pkg/sshtunnel"
)

const (
	environmentIDPrefix        = "env"
	templateDerivativeIDPrefix = "tpld"
)

// EnvironmentRoute identifies the node-local derivative backing a source environment route.
type EnvironmentRoute struct {
	Source                  environment.Environment
	NodeURL                 string
	DerivativeEnvironmentID string
}

type templateDerivative struct {
	ID                   string
	TemplateID           string
	NodeID               string
	DerivativeTemplateID string
	CreatedAt            string
}

type clusterEnvironmentRecord struct {
	environment.Environment
	NodeID                  string
	NodeURL                 string
	DerivativeID            string
	DerivativeEnvironmentID string
	DerivativeTemplateID    string
}

// CreateEnvironment creates a source environment in a namespace and a derivative environment on a selected node.
//
//nolint:gocyclo // Coordinates namespace validation, scheduling, derivative provisioning, persistence, and cleanup.
func (s *Service) CreateEnvironment(ctx context.Context, namespaceSelector NamespaceSelector, req environment.CreateRequest) (environment.Environment, error) {
	if err := writeClusterProgress(req.Logs, "resolving namespace"); err != nil {
		return environment.Environment{}, err
	}

	namespace, err := s.resolveNamespace(ctx, namespaceSelector)
	if err != nil {
		return environment.Environment{}, err
	}

	if err := writeClusterProgress(req.Logs, "validating environment request"); err != nil {
		return environment.Environment{}, err
	}

	if err := validateEnvironmentCreateRequest(req); err != nil {
		return environment.Environment{}, err
	}

	if err := writeClusterProgress(req.Logs, "checking template archive storage"); err != nil {
		return environment.Environment{}, err
	}

	if err := s.requireArchiveStore(); err != nil {
		return environment.Environment{}, err
	}

	if err := writeClusterProgress(req.Logs, "resolving source template"); err != nil {
		return environment.Environment{}, err
	}

	sourceTemplate, archiveKey, _, _, err := s.getTemplateInNamespace(ctx, namespace.ID, req.TemplateID, req.TemplateKey)
	if err != nil {
		return environment.Environment{}, err
	}

	if err := writeClusterProgress(req.Logs, "resolving template resource requirements"); err != nil {
		return environment.Environment{}, err
	}

	usage, err := ch.ResolveTemplateResourceUsage(sourceTemplate.Config)
	if err != nil {
		return environment.Environment{}, fmt.Errorf("%w: resolve source template resources: %w", failure.ErrInvalid, err)
	}

	if err := writeClusterProgress(req.Logs, "selecting environment node"); err != nil {
		return environment.Environment{}, err
	}

	node, derivative, derivativeExists, err := s.selectEnvironmentNode(ctx, sourceTemplate.ID, usage, req.Logs)
	if err != nil {
		return environment.Environment{}, err
	}

	createdDerivative := false
	if !derivativeExists {
		if err := writeClusterProgress(req.Logs, "creating template derivative on cluster node %s", node.ID); err != nil {
			return environment.Environment{}, err
		}

		derivative, err = s.createTemplateDerivative(ctx, namespace.ID, node, sourceTemplate, archiveKey, req.Logs)
		if err != nil {
			return environment.Environment{}, err
		}

		createdDerivative = true
	} else if err := writeClusterProgress(req.Logs, "reusing template derivative on cluster node %s", node.ID); err != nil {
		return environment.Environment{}, err
	}

	environmentID, err := services.GenerateID(environmentIDPrefix)
	if err != nil {
		return environment.Environment{}, err
	}

	now := services.Now()
	source := environment.Environment{ID: environmentID, Key: services.CopyStringPtr(req.Key), Status: ch.StateCreating, TemplateID: sourceTemplate.ID, Tags: copyEnvironmentTags(req.Tags), CreatedAt: now, UpdatedAt: now}

	if err := writeClusterProgress(req.Logs, "creating derivative environment on cluster node %s", node.ID); err != nil {
		return environment.Environment{}, err
	}

	derivativeEnvironment, err := s.nodeClient.CreateEnvironment(ctx, node.URL, environment.CreateRequest{TemplateID: derivative.DerivativeTemplateID, Tags: req.Tags, Logs: req.Logs})
	if err != nil {
		if createdDerivative {
			_ = s.removeTemplateDerivative(context.Background(), node.URL, derivative)
		}

		return environment.Environment{}, fmt.Errorf("%w: create derivative environment on cluster node %s: %w", failure.ErrFailedDependency, node.ID, err)
	}

	if derivativeEnvironment.Status != "" {
		source.Status = derivativeEnvironment.Status
	} else {
		source.Status = ch.StateRunning
	}
	source.LastError = derivativeEnvironment.LastError

	if err := writeClusterProgress(req.Logs, "recording source environment"); err != nil {
		return environment.Environment{}, err
	}

	if err := s.insertEnvironment(ctx, namespace.ID, source, node.ID, derivative.ID, derivativeEnvironment.ID); err != nil {
		_, _ = s.nodeClient.RemoveEnvironment(context.Background(), node.URL, derivativeEnvironment.ID)
		if createdDerivative {
			_ = s.removeTemplateDerivative(context.Background(), node.URL, derivative)
		}

		return environment.Environment{}, err
	}

	return source, nil
}

// ListEnvironments returns source environments scoped to a namespace.
func (s *Service) ListEnvironments(ctx context.Context, namespaceSelector NamespaceSelector, limit int, cursor string, tags []string) (services.Page[environment.Environment], error) {
	namespace, err := s.resolveNamespace(ctx, namespaceSelector)
	if err != nil {
		return services.Page[environment.Environment]{}, err
	}

	if err := validateEnvironmentTags(tags); err != nil {
		return services.Page[environment.Environment]{}, err
	}

	limit = services.NormalizeLimit(limit)
	query, args := environmentListQuery(namespace.ID, limit, cursor, tags)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return services.Page[environment.Environment]{}, fmt.Errorf("list cluster environments: %w", err)
	}
	defer rows.Close()

	records, err := scanClusterEnvironmentRecords(rows, limit+1)
	if err != nil {
		return services.Page[environment.Environment]{}, err
	}

	entries := make([]environment.Environment, 0, len(records))
	for _, record := range records {
		entries = append(entries, record.Environment)
	}

	if err := s.loadEnvironmentTags(ctx, entries); err != nil {
		return services.Page[environment.Environment]{}, err
	}

	for i := range records {
		records[i].Environment = entries[i]
		reconciled, err := s.reconcileEnvironment(ctx, records[i])
		if err != nil {
			return services.Page[environment.Environment]{}, err
		}

		entries[i] = reconciled
	}

	return services.FromEntries(entries, limit, func(entry environment.Environment) string { return entry.CreatedAt }), nil
}

// GetEnvironment returns a source environment scoped to a namespace by ID or key.
func (s *Service) GetEnvironment(ctx context.Context, namespaceSelector NamespaceSelector, environmentID, key string) (environment.Environment, error) {
	record, err := s.getEnvironmentRecord(ctx, namespaceSelector, environmentID, key)
	if err != nil {
		return environment.Environment{}, err
	}

	return s.reconcileEnvironment(ctx, record)
}

// RemoveEnvironment removes a source environment and its derivative environment.
func (s *Service) RemoveEnvironment(ctx context.Context, namespaceSelector NamespaceSelector, environmentID, key string) (environment.Environment, error) {
	record, err := s.getEnvironmentRecord(ctx, namespaceSelector, environmentID, key)
	if err != nil {
		return environment.Environment{}, err
	}

	if _, err := s.nodeClient.RemoveEnvironment(ctx, record.NodeURL, record.DerivativeEnvironmentID); err != nil && !errors.Is(err, failure.ErrNotFound) {
		return environment.Environment{}, fmt.Errorf("%w: remove derivative environment on cluster node %s: %w", failure.ErrFailedDependency, record.NodeID, err)
	}

	if _, err := s.db.Exec(ctx, `DELETE FROM cluster_environments WHERE id = $1`, record.ID); err != nil {
		return environment.Environment{}, fmt.Errorf("remove cluster environment: %w", err)
	}

	if err := s.cleanupUnusedDerivative(ctx, record.DerivativeID); err != nil {
		return environment.Environment{}, err
	}

	removed := record.Environment
	removed.Status = "removed"
	removed.UpdatedAt = services.Now()
	removed.LastError = ""

	return removed, nil
}

// GetEnvironmentTunnels returns source template tunnel metadata for a source environment.
func (s *Service) GetEnvironmentTunnels(ctx context.Context, namespaceSelector NamespaceSelector, environmentID, key string) (environment.Tunnels, error) {
	source, err := s.GetEnvironment(ctx, namespaceSelector, environmentID, key)
	if err != nil {
		return environment.Tunnels{}, err
	}

	if source.Status != ch.StateRunning {
		return environment.Tunnels{}, fmt.Errorf("%w: environment status is %q, want running", failure.ErrFailedDependency, source.Status)
	}

	namespace, err := s.resolveNamespace(ctx, namespaceSelector)
	if err != nil {
		return environment.Tunnels{}, err
	}

	sourceTemplate, _, _, _, err := s.getTemplateInNamespace(ctx, namespace.ID, source.TemplateID, "")
	if err != nil {
		return environment.Tunnels{}, err
	}

	entries, err := environmentTunnelsFromConfig(sourceTemplate.Config)
	if err != nil {
		return environment.Tunnels{}, fmt.Errorf("%w: %w", failure.ErrFailedDependency, err)
	}

	return environment.Tunnels{Entries: entries}, nil
}

// GetEnvironmentRoute returns node routing metadata for proxying a source environment path.
func (s *Service) GetEnvironmentRoute(ctx context.Context, namespaceSelector NamespaceSelector, environmentID, key string) (EnvironmentRoute, error) {
	record, err := s.getEnvironmentRecord(ctx, namespaceSelector, environmentID, key)
	if err != nil {
		return EnvironmentRoute{}, err
	}

	reconciled, err := s.reconcileEnvironment(ctx, record)
	if err != nil {
		return EnvironmentRoute{}, err
	}

	return EnvironmentRoute{Source: reconciled, NodeURL: record.NodeURL, DerivativeEnvironmentID: record.DerivativeEnvironmentID}, nil
}

// OpenEnvironmentSSH opens a proxied SSH stream to a source environment derivative.
func (s *Service) OpenEnvironmentSSH(ctx context.Context, namespaceSelector NamespaceSelector, environmentID, key string, req sshtunnel.Request) (io.ReadWriteCloser, error) {
	route, err := s.GetEnvironmentRoute(ctx, namespaceSelector, environmentID, key)
	if err != nil {
		return nil, err
	}

	return s.nodeClient.OpenSSH(ctx, route.NodeURL, route.DerivativeEnvironmentID, req)
}

func validateEnvironmentCreateRequest(req environment.CreateRequest) error {
	if err := services.RequireIDOrKey(req.TemplateID, req.TemplateKey); err != nil {
		return err
	}

	if err := services.ValidateOptionalKey("environment", req.Key); err != nil {
		return err
	}

	return validateEnvironmentTags(req.Tags)
}

func validateEnvironmentTags(tags []string) error {
	for _, tag := range tags {
		if strings.TrimSpace(tag) == "" {
			return fmt.Errorf("%w: environment tag is required", failure.ErrInvalid)
		}
	}

	return nil
}

//nolint:gocyclo // Checks existing derivatives and fallback nodes while streaming scheduling progress.
func (s *Service) selectEnvironmentNode(ctx context.Context, templateID string, usage ch.ResourceUsage, logs io.Writer) (Node, templateDerivative, bool, error) {
	if err := writeClusterProgress(logs, "checking existing template derivatives"); err != nil {
		return Node{}, templateDerivative{}, false, err
	}

	derivatives, err := s.listTemplateDerivatives(ctx, templateID)
	if err != nil {
		return Node{}, templateDerivative{}, false, err
	}

	for _, derivative := range derivatives {
		node, err := s.GetNode(ctx, derivative.NodeID, "")
		if err != nil {
			return Node{}, templateDerivative{}, false, err
		}

		if err := writeClusterProgress(logs, "checking cluster node capacity"); err != nil {
			return Node{}, templateDerivative{}, false, err
		}

		hasCapacity, err := s.nodeHasCapacity(ctx, node, usage)
		if err != nil {
			return Node{}, templateDerivative{}, false, err
		}

		if hasCapacity {
			return node, derivative, true, nil
		}
	}

	nodes, err := s.allNodes(ctx)
	if err != nil {
		return Node{}, templateDerivative{}, false, err
	}

	if len(nodes) == 0 {
		return Node{}, templateDerivative{}, false, fmt.Errorf("%w: no cluster nodes are registered", failure.ErrFailedDependency)
	}

	randomNodes := append([]Node(nil), nodes...)
	rand.Shuffle(len(randomNodes), func(i, j int) { randomNodes[i], randomNodes[j] = randomNodes[j], randomNodes[i] })

	for _, node := range randomNodes {
		if err := writeClusterProgress(logs, "checking cluster node capacity"); err != nil {
			return Node{}, templateDerivative{}, false, err
		}

		hasCapacity, err := s.nodeHasCapacity(ctx, node, usage)
		if err != nil {
			return Node{}, templateDerivative{}, false, err
		}

		if hasCapacity {
			return node, templateDerivative{}, false, nil
		}
	}

	return Node{}, templateDerivative{}, false, fmt.Errorf("%w: cluster does not have enough capacity to create environment", failure.ErrFailedDependency)
}

func (s *Service) nodeHasCapacity(ctx context.Context, node Node, usage ch.ResourceUsage) (bool, error) {
	current, err := s.nodeClient.Utilization(ctx, node.URL)
	if err != nil {
		return false, fmt.Errorf("%w: cluster node %s utilization failed: %w", failure.ErrFailedDependency, node.ID, err)
	}

	sourceUsed, err := s.nodeSourceResourceUsage(ctx, node.ID)
	if err != nil {
		return false, err
	}

	return resourceHasCapacity(current.VCPU, sourceUsed.VCPU, usage.VCPU) && resourceHasCapacity(current.Memory, sourceUsed.MemoryBytes, usage.MemoryBytes) && resourceHasCapacity(current.Volume, sourceUsed.VolumeBytes, usage.VolumeBytes), nil
}

func (s *Service) nodeSourceResourceUsage(ctx context.Context, nodeID string) (ch.ResourceUsage, error) {
	rows, err := s.db.Query(ctx, `
SELECT e.id, t.config
FROM cluster_environments e
JOIN cluster_templates t ON t.id = e.template_id
WHERE e.node_id = $1 AND e.status IN ($2, $3, $4)
`, nodeID, ch.StateCreating, ch.StateRunning, ch.StatePaused)
	if err != nil {
		return ch.ResourceUsage{}, fmt.Errorf("query cluster node source resource usage: %w", err)
	}
	defer rows.Close()

	var used ch.ResourceUsage
	for rows.Next() {
		var environmentID, config string
		if err := rows.Scan(&environmentID, &config); err != nil {
			return ch.ResourceUsage{}, fmt.Errorf("scan cluster node source resource usage: %w", err)
		}

		usage, err := ch.ResolveTemplateResourceUsage(json.RawMessage(config))
		if err != nil {
			return ch.ResourceUsage{}, fmt.Errorf("resolve resource usage for cluster environment %s: %w", environmentID, err)
		}

		used.VCPU += usage.VCPU
		used.MemoryBytes += usage.MemoryBytes
		used.VolumeBytes += usage.VolumeBytes
	}

	if err := rows.Err(); err != nil {
		return ch.ResourceUsage{}, fmt.Errorf("iterate cluster node source resource usage: %w", err)
	}

	return used, nil
}

func resourceHasCapacity(resource utilization.Resource, sourceUsed, required int64) bool {
	available := max(resource.Total-max(resource.Used, sourceUsed), int64(0))

	return available >= required
}

//nolint:gocyclo // Coordinates archive restore/rewrite/import, DB persistence, cleanup, and streamed progress.
func (s *Service) createTemplateDerivative(ctx context.Context, namespaceID string, node Node, sourceTemplate template.Template, archiveKey string, logs io.Writer) (templateDerivative, error) {
	derivatives := derivativeCleanup{service: s, nodeURL: node.URL}
	cleanupDerivatives := true
	defer func() {
		if cleanupDerivatives {
			_ = derivatives.cleanup(context.Background())
		}
	}()

	if err := writeClusterProgress(logs, "preparing derivative secrets"); err != nil {
		return templateDerivative{}, err
	}

	derivativeConfig, sourceDerivativeSecrets, err := s.createDerivativeSecrets(ctx, namespaceID, node, sourceTemplate.Config, &derivatives, logs)
	if err != nil {
		return templateDerivative{}, err
	}

	sourceArchive, err := createTempFile("bastion-cluster-environment-source-*.tar.zst")
	if err != nil {
		return templateDerivative{}, err
	}
	defer sourceArchive.cleanup()

	if err := writeClusterProgress(logs, "loading source template archive"); err != nil {
		return templateDerivative{}, err
	}

	if err := s.archiveStore.Get(ctx, archiveKey, sourceArchive.file); err != nil {
		return templateDerivative{}, fmt.Errorf("get source template archive: %w", err)
	}

	rewrittenArchive, err := createTempFile("bastion-cluster-environment-derivative-*.tar.zst")
	if err != nil {
		return templateDerivative{}, err
	}
	defer rewrittenArchive.cleanup()

	if _, err := sourceArchive.file.Seek(0, io.SeekStart); err != nil {
		return templateDerivative{}, fmt.Errorf("rewind source template archive: %w", err)
	}

	archiveTemplate := derivativeArchiveTemplate(sourceTemplate, derivativeConfig)
	if err := writeClusterProgress(logs, "rewriting template derivative archive"); err != nil {
		return templateDerivative{}, err
	}

	if err := templatearchive.RewriteTemplate(ctx, sourceArchive.file, rewrittenArchive.file, archiveTemplate); err != nil {
		return templateDerivative{}, fmt.Errorf("rewrite derivative template archive: %w", err)
	}

	archiveSize, err := tempFileSize(rewrittenArchive.file)
	if err != nil {
		return templateDerivative{}, err
	}

	if _, err := rewrittenArchive.file.Seek(0, io.SeekStart); err != nil {
		return templateDerivative{}, fmt.Errorf("rewind derivative template archive: %w", err)
	}

	if err := writeClusterProgress(logs, "importing template derivative"); err != nil {
		return templateDerivative{}, err
	}

	imported, err := s.nodeClient.ImportTemplate(ctx, node.URL, template.ImportRequest{Archive: rewrittenArchive.file, ArchiveSize: archiveSize})
	if err != nil {
		return templateDerivative{}, fmt.Errorf("%w: import derivative template on cluster node %s: %w", failure.ErrFailedDependency, node.ID, err)
	}

	derivatives.templateID = imported.ID

	derivativeID, err := services.GenerateID(templateDerivativeIDPrefix)
	if err != nil {
		return templateDerivative{}, err
	}

	derivative := templateDerivative{ID: derivativeID, TemplateID: sourceTemplate.ID, NodeID: node.ID, DerivativeTemplateID: imported.ID, CreatedAt: services.Now()}
	if err := writeClusterProgress(logs, "recording template derivative"); err != nil {
		return templateDerivative{}, err
	}

	if err := s.insertTemplateDerivative(ctx, derivative, sourceDerivativeSecrets); err != nil {
		return templateDerivative{}, err
	}

	cleanupDerivatives = false

	return derivative, nil
}

func derivativeArchiveTemplate(sourceTemplate template.Template, derivativeConfig json.RawMessage) templatearchive.Template {
	return templatearchive.Template{ID: sourceTemplate.ID, Config: derivativeConfig, BaseContentAddress: sourceTemplate.BaseContentAddress}
}

func tempFileSize(file *os.File) (int64, error) {
	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("stat temporary template archive: %w", err)
	}

	return info.Size(), nil
}

func (s *Service) insertTemplateDerivative(ctx context.Context, derivative templateDerivative, derivativeSecrets []derivativeSecretMapping) error {
	_, err := s.db.Exec(ctx, `INSERT INTO cluster_template_derivatives (id, template_id, node_id, derivative_template_id, created_at) VALUES ($1, $2, $3, $4, $5)`, derivative.ID, derivative.TemplateID, derivative.NodeID, derivative.DerivativeTemplateID, derivative.CreatedAt)
	if err != nil {
		if clusterdb.IsConstraint(err) {
			return fmt.Errorf("%w: template derivative already exists", failure.ErrConflict)
		}

		return fmt.Errorf("create cluster template derivative: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_, _ = s.db.Exec(context.Background(), `DELETE FROM cluster_template_derivatives WHERE id = $1`, derivative.ID)
		}
	}()

	for position, mapping := range derivativeSecrets {
		if _, err := s.db.Exec(ctx, `INSERT INTO cluster_template_derivative_node_secrets (derivative_id, source_secret_id, derivative_secret_id, position) VALUES ($1, $2, $3, $4)`, derivative.ID, mapping.sourceID, mapping.derivativeID, position); err != nil {
			return fmt.Errorf("record template derivative secret: %w", err)
		}
	}

	committed = true

	return nil
}

func (s *Service) insertEnvironment(ctx context.Context, namespaceID string, source environment.Environment, nodeID, derivativeID, derivativeEnvironmentID string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO cluster_environments (id, namespace_id, key, status, template_id, node_id, derivative_id, derivative_environment_id, created_at, updated_at, last_error) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, source.ID, namespaceID, services.OptionalStringValue(source.Key), source.Status, source.TemplateID, nodeID, derivativeID, derivativeEnvironmentID, source.CreatedAt, source.UpdatedAt, source.LastError)
	if err != nil {
		if clusterdb.IsConstraint(err) {
			return fmt.Errorf("%w: environment already exists", failure.ErrConflict)
		}

		return fmt.Errorf("create cluster environment: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_, _ = s.db.Exec(context.Background(), `DELETE FROM cluster_environments WHERE id = $1`, source.ID)
		}
	}()

	for position, tag := range source.Tags {
		if _, err := s.db.Exec(ctx, `INSERT INTO cluster_environment_tags (environment_id, tag, position) VALUES ($1, $2, $3)`, source.ID, tag, position); err != nil {
			return fmt.Errorf("create cluster environment tag: %w", err)
		}
	}

	committed = true

	return nil
}

func (s *Service) listTemplateDerivatives(ctx context.Context, templateID string) ([]templateDerivative, error) {
	rows, err := s.db.Query(ctx, `SELECT id, template_id, node_id, derivative_template_id, created_at FROM cluster_template_derivatives WHERE template_id = $1 ORDER BY created_at`, templateID)
	if err != nil {
		return nil, fmt.Errorf("list cluster template derivatives: %w", err)
	}
	defer rows.Close()

	return scanTemplateDerivatives(rows, 0)
}

func (s *Service) getTemplateDerivative(ctx context.Context, derivativeID string) (templateDerivative, error) {
	derivative, err := scanTemplateDerivative(s.db.QueryRow(ctx, `SELECT id, template_id, node_id, derivative_template_id, created_at FROM cluster_template_derivatives WHERE id = $1`, derivativeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return templateDerivative{}, fmt.Errorf("%w: template derivative not found", failure.ErrNotFound)
	}

	if err != nil {
		return templateDerivative{}, fmt.Errorf("get template derivative: %w", err)
	}

	return derivative, nil
}

func scanTemplateDerivative(row scanner) (templateDerivative, error) {
	var derivative templateDerivative
	if err := row.Scan(&derivative.ID, &derivative.TemplateID, &derivative.NodeID, &derivative.DerivativeTemplateID, &derivative.CreatedAt); err != nil {
		return templateDerivative{}, err
	}

	return derivative, nil
}

func scanTemplateDerivatives(rows pgx.Rows, capacity int) ([]templateDerivative, error) {
	derivatives := make([]templateDerivative, 0, capacity)
	for rows.Next() {
		derivative, err := scanTemplateDerivative(rows)
		if err != nil {
			return nil, fmt.Errorf("scan cluster template derivative: %w", err)
		}

		derivatives = append(derivatives, derivative)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cluster template derivatives: %w", err)
	}

	return derivatives, nil
}

func environmentListQuery(namespaceID string, limit int, cursor string, tags []string) (string, []any) {
	query := environmentRecordSelectQuery() + ` WHERE e.namespace_id = $1`
	args := []any{namespaceID}
	filters := []string{}

	if cursor != "" {
		args = append(args, cursor)
		filters = append(filters, fmt.Sprintf("e.created_at > $%d", len(args)))
	}

	for index, tag := range uniqueEnvironmentTags(tags) {
		args = append(args, tag)
		alias := fmt.Sprintf("tag_filter_%d", index)
		filters = append(filters, fmt.Sprintf("EXISTS (SELECT 1 FROM cluster_environment_tags %s WHERE %s.environment_id = e.id AND %s.tag = $%d)", alias, alias, alias, len(args)))
	}

	if len(filters) > 0 {
		query += ` AND ` + strings.Join(filters, ` AND `)
	}

	args = append(args, limit+1)
	query += fmt.Sprintf(` ORDER BY e.created_at LIMIT $%d`, len(args))

	return query, args
}

func (s *Service) getEnvironmentRecord(ctx context.Context, namespaceSelector NamespaceSelector, environmentID, key string) (clusterEnvironmentRecord, error) {
	namespace, err := s.resolveNamespace(ctx, namespaceSelector)
	if err != nil {
		return clusterEnvironmentRecord{}, err
	}

	if err := services.RequireIDOrKey(environmentID, key); err != nil {
		return clusterEnvironmentRecord{}, err
	}

	var (
		record  clusterEnvironmentRecord
		scanErr error
	)
	if environmentID != "" {
		record, scanErr = scanClusterEnvironmentRecord(s.db.QueryRow(ctx, environmentRecordSelectQuery()+` WHERE e.namespace_id = $1 AND e.id = $2`, namespace.ID, environmentID))
	} else {
		record, scanErr = scanClusterEnvironmentRecord(s.db.QueryRow(ctx, environmentRecordSelectQuery()+` WHERE e.namespace_id = $1 AND e.key = $2`, namespace.ID, key))
	}

	if errors.Is(scanErr, pgx.ErrNoRows) {
		return clusterEnvironmentRecord{}, fmt.Errorf("%w: environment not found", failure.ErrNotFound)
	}

	if scanErr != nil {
		return clusterEnvironmentRecord{}, fmt.Errorf("get cluster environment: %w", scanErr)
	}

	entries := []environment.Environment{record.Environment}
	if err := s.loadEnvironmentTags(ctx, entries); err != nil {
		return clusterEnvironmentRecord{}, err
	}
	record.Environment = entries[0]

	return record, nil
}

func environmentRecordSelectQuery() string {
	return `
SELECT
  e.id, e.key, e.status, e.template_id, e.created_at, e.updated_at, e.last_error,
  e.node_id, n.url, e.derivative_id, e.derivative_environment_id, td.derivative_template_id
FROM cluster_environments e
JOIN cluster_nodes n ON n.id = e.node_id
JOIN cluster_template_derivatives td ON td.id = e.derivative_id`
}

func scanClusterEnvironmentRecord(row scanner) (clusterEnvironmentRecord, error) {
	var (
		record clusterEnvironmentRecord
		key    sql.NullString
	)

	if err := row.Scan(
		&record.ID,
		&key,
		&record.Status,
		&record.TemplateID,
		&record.CreatedAt,
		&record.UpdatedAt,
		&record.LastError,
		&record.NodeID,
		&record.NodeURL,
		&record.DerivativeID,
		&record.DerivativeEnvironmentID,
		&record.DerivativeTemplateID,
	); err != nil {
		return clusterEnvironmentRecord{}, err
	}

	record.Key = services.NullStringPtr(key)
	record.Tags = []string{}

	return record, nil
}

func scanClusterEnvironmentRecords(rows pgx.Rows, capacity int) ([]clusterEnvironmentRecord, error) {
	records := make([]clusterEnvironmentRecord, 0, capacity)
	for rows.Next() {
		record, err := scanClusterEnvironmentRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("scan cluster environment: %w", err)
		}

		records = append(records, record)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cluster environments: %w", err)
	}

	return records, nil
}

func (s *Service) loadEnvironmentTags(ctx context.Context, entries []environment.Environment) error {
	if len(entries) == 0 {
		return nil
	}

	placeholders := make([]string, len(entries))
	args := make([]any, len(entries))
	indices := make(map[string]int, len(entries))
	for i := range entries {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = entries[i].ID
		indices[entries[i].ID] = i
		entries[i].Tags = []string{}
	}

	rows, err := s.db.Query(ctx, `SELECT environment_id, tag FROM cluster_environment_tags WHERE environment_id IN (`+strings.Join(placeholders, ",")+`) ORDER BY environment_id, position`, args...)
	if err != nil {
		return fmt.Errorf("list cluster environment tags: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var environmentID, tag string
		if err := rows.Scan(&environmentID, &tag); err != nil {
			return fmt.Errorf("scan cluster environment tag: %w", err)
		}

		if index, ok := indices[environmentID]; ok {
			entries[index].Tags = append(entries[index].Tags, tag)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate cluster environment tags: %w", err)
	}

	return nil
}

func (s *Service) reconcileEnvironment(ctx context.Context, record clusterEnvironmentRecord) (environment.Environment, error) {
	if record.Status == "removing" || record.Status == "removed" {
		return record.Environment, nil
	}

	derivative, err := s.nodeClient.GetEnvironment(ctx, record.NodeURL, record.DerivativeEnvironmentID)
	if errors.Is(err, failure.ErrNotFound) {
		updated := record.Environment
		updated.Status = ch.StateStopped
		updated.LastError = ""
		updated.UpdatedAt = services.Now()
		if updateErr := s.updateEnvironmentStatus(ctx, updated.ID, updated.Status, updated.LastError, updated.UpdatedAt); updateErr != nil {
			return environment.Environment{}, updateErr
		}

		return updated, nil
	}

	if err != nil {
		return environment.Environment{}, fmt.Errorf("%w: reconcile derivative environment on cluster node %s: %w", failure.ErrFailedDependency, record.NodeID, err)
	}

	updated := record.Environment
	if derivative.Status != "" {
		updated.Status = derivative.Status
	}
	updated.LastError = derivative.LastError

	if updated.Status != record.Status || updated.LastError != record.LastError {
		updated.UpdatedAt = services.Now()
		if err := s.updateEnvironmentStatus(ctx, updated.ID, updated.Status, updated.LastError, updated.UpdatedAt); err != nil {
			return environment.Environment{}, err
		}
	}

	return updated, nil
}

func (s *Service) updateEnvironmentStatus(ctx context.Context, environmentID, status, lastError, updatedAt string) error {
	_, err := s.db.Exec(ctx, `UPDATE cluster_environments SET status = $1, updated_at = $2, last_error = $3 WHERE id = $4`, status, updatedAt, lastError, environmentID)
	if err != nil {
		return fmt.Errorf("update cluster environment status: %w", err)
	}

	return nil
}

func (s *Service) cleanupUnusedDerivative(ctx context.Context, derivativeID string) error {
	var count int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM cluster_environments WHERE derivative_id = $1`, derivativeID).Scan(&count); err != nil {
		return fmt.Errorf("count derivative environment usage: %w", err)
	}

	if count > 0 {
		return nil
	}

	derivative, err := s.getTemplateDerivative(ctx, derivativeID)
	if err != nil {
		return err
	}

	node, err := s.GetNode(ctx, derivative.NodeID, "")
	if err != nil {
		return err
	}

	return s.removeTemplateDerivative(ctx, node.URL, derivative)
}

func (s *Service) removeTemplateDerivative(ctx context.Context, nodeURL string, derivative templateDerivative) error {
	if err := s.cleanupTemplateDerivative(ctx, nodeURL, derivative); err != nil {
		return err
	}

	if _, err := s.db.Exec(ctx, `DELETE FROM cluster_template_derivatives WHERE id = $1`, derivative.ID); err != nil {
		return fmt.Errorf("remove cluster template derivative: %w", err)
	}

	return nil
}

func (s *Service) cleanupTemplateDerivative(ctx context.Context, nodeURL string, derivative templateDerivative) error {
	var joined error

	if derivative.DerivativeTemplateID != "" {
		if err := s.nodeClient.RemoveTemplate(ctx, nodeURL, derivative.DerivativeTemplateID); err != nil && !errors.Is(err, failure.ErrNotFound) {
			joined = errors.Join(joined, fmt.Errorf("remove derivative template: %w", err))
		}
	}

	secretIDs, err := s.templateDerivativeSecretIDs(ctx, derivative.ID)
	if err != nil {
		joined = errors.Join(joined, err)
	}

	for _, secretID := range secretIDs {
		if err := s.nodeClient.RemoveSecret(ctx, nodeURL, secretID); err != nil && !errors.Is(err, failure.ErrNotFound) {
			joined = errors.Join(joined, fmt.Errorf("remove derivative secret %s: %w", secretID, err))
		}
	}

	if joined != nil {
		return fmt.Errorf("%w: cleanup derivative resources: %w", failure.ErrFailedDependency, joined)
	}

	return nil
}

func (s *Service) templateDerivativeSecretIDs(ctx context.Context, derivativeID string) ([]string, error) {
	rows, err := s.db.Query(ctx, `SELECT derivative_secret_id FROM cluster_template_derivative_node_secrets WHERE derivative_id = $1 ORDER BY position`, derivativeID)
	if err != nil {
		return nil, fmt.Errorf("list derivative secrets: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan derivative secret: %w", err)
		}

		ids = append(ids, id)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate derivative secrets: %w", err)
	}

	return ids, nil
}

func environmentTunnelsFromConfig(config json.RawMessage) ([]environment.Tunnel, error) {
	var parsed struct {
		Tunnels map[string]int `json:"tunnels,omitempty"`
	}

	if err := json.Unmarshal(config, &parsed); err != nil {
		return nil, fmt.Errorf("parse template config: %w", err)
	}

	entries := make([]environment.Tunnel, 0, len(parsed.Tunnels))
	for name, port := range parsed.Tunnels {
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("tunnel %s port %d is out of range", name, port)
		}

		entries = append(entries, environment.Tunnel{Name: name, Port: port})
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	return entries, nil
}

func copyEnvironmentTags(tags []string) []string {
	if len(tags) == 0 {
		return []string{}
	}

	return append([]string(nil), tags...)
}

func uniqueEnvironmentTags(tags []string) []string {
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
