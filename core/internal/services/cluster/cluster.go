// Package cluster manages cluster control plane resources.
package cluster

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/bastion-computer/bastion/core/internal/clusterdb"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/base"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/secret"
	"github.com/bastion-computer/bastion/core/internal/services/template"
	"github.com/bastion-computer/bastion/core/internal/services/utilization"
	"github.com/bastion-computer/bastion/core/pkg/sshtunnel"
)

const (
	nodeIDPrefix      = "node"
	namespaceIDPrefix = "ns"
	nodeClientTimeout = 30 * time.Minute
)

// Health describes aggregate cluster health.
type Health struct {
	Status string `json:"status"`
}

// Node describes a Bastion API node in the cluster.
type Node struct {
	ID        string  `json:"id"`
	Key       *string `json:"key,omitempty"`
	URL       string  `json:"url"`
	CreatedAt string  `json:"createdAt"`
}

// CreateNodeRequest contains the fields needed to add a node to the cluster.
type CreateNodeRequest struct {
	Key  *string   `json:"key,omitempty"`
	URL  string    `json:"url"`
	Logs io.Writer `json:"-"`
}

// Stream event types used by streaming cluster operations.
const (
	StreamEventLog    = "log"
	StreamEventResult = "result"
	StreamEventError  = "error"
)

// NodeStreamEvent is one line in a streamed cluster node creation response.
type NodeStreamEvent struct {
	Type   string `json:"type"`
	Log    string `json:"log,omitempty"`
	Node   *Node  `json:"node,omitempty"`
	Error  string `json:"error,omitempty"`
	Status int    `json:"status,omitempty"`
}

// Namespace describes a resource isolation namespace in the cluster.
type Namespace struct {
	ID        string  `json:"id"`
	Key       *string `json:"key,omitempty"`
	CreatedAt string  `json:"createdAt"`
}

// CreateNamespaceRequest contains the fields needed to create a namespace.
type CreateNamespaceRequest struct {
	Key *string `json:"key,omitempty"`
}

// NodeClient calls underlying Bastion API nodes.
type NodeClient interface {
	Health(context.Context, string) error
	Utilization(context.Context, string) (utilization.Utilization, error)
	GetBase(context.Context, string) (base.Base, error)
	BuildBase(context.Context, string, base.BuildRequest) (base.Base, error)
	ImportBase(context.Context, string, base.ImportRequest) (base.Base, error)
	ExportBase(context.Context, string, io.Writer) error
	CreateSecret(context.Context, string, secret.CreateRequest) (secret.Metadata, error)
	RemoveSecret(context.Context, string, string) error
	CreateTemplate(context.Context, string, template.CreateRequest) (template.Metadata, error)
	ImportTemplate(context.Context, string, template.ImportRequest) (template.Metadata, error)
	ExportTemplate(context.Context, string, string, io.Writer) error
	RemoveTemplate(context.Context, string, string) error
	CreateEnvironment(context.Context, string, environment.CreateRequest) (environment.Environment, error)
	GetEnvironment(context.Context, string, string) (environment.Environment, error)
	RemoveEnvironment(context.Context, string, string) (environment.Environment, error)
	OpenSSH(context.Context, string, string, sshtunnel.Request) (io.ReadWriteCloser, error)
}

func writeClusterProgress(logs io.Writer, message string, args ...any) error {
	if logs == nil {
		return nil
	}

	if len(args) > 0 {
		message = fmt.Sprintf(message, args...)
	}

	if _, err := fmt.Fprintf(logs, "cluster: %s\n", message); err != nil {
		return fmt.Errorf("stream cluster progress: %w", err)
	}

	return nil
}

// Option configures the cluster service.
type Option func(*Service)

// Service manages cluster control plane state.
type Service struct {
	db           *clusterdb.Client
	nodeClient   NodeClient
	archiveStore TemplateArchiveStore
}

// NewService returns a cluster service backed by db.
func NewService(db *clusterdb.Client, opts ...Option) *Service {
	service := &Service{db: db, nodeClient: HTTPNodeClient{Client: &http.Client{Timeout: nodeClientTimeout}}}
	for _, opt := range opts {
		opt(service)
	}

	if service.nodeClient == nil {
		service.nodeClient = HTTPNodeClient{Client: &http.Client{Timeout: nodeClientTimeout}}
	}

	return service
}

// WithNodeClient configures how the service calls underlying Bastion API nodes.
func WithNodeClient(client NodeClient) Option {
	return func(s *Service) {
		s.nodeClient = client
	}
}

// WithTemplateArchiveStore configures persistent storage for cluster template exports.
func WithTemplateArchiveStore(store TemplateArchiveStore) Option {
	return func(s *Service) {
		s.archiveStore = store
	}
}

// CreateNode stores a cluster node after synchronizing the current cluster base, if any.
func (s *Service) CreateNode(ctx context.Context, req CreateNodeRequest) (Node, error) {
	if err := validateOptionalKey("cluster node", nodeIDPrefix, req.Key); err != nil {
		return Node{}, err
	}

	if err := validateNodeURL(req.URL); err != nil {
		return Node{}, err
	}

	nodeID, err := services.GenerateID(nodeIDPrefix)
	if err != nil {
		return Node{}, err
	}

	node := Node{ID: nodeID, Key: services.CopyStringPtr(req.Key), URL: req.URL, CreatedAt: services.Now()}

	if err := s.syncBaseToNewNode(ctx, node, req.Logs); err != nil {
		return Node{}, err
	}

	_, err = s.db.Exec(ctx, `INSERT INTO cluster_nodes (id, key, url, created_at) VALUES ($1, $2, $3, $4)`, node.ID, services.OptionalStringValue(node.Key), node.URL, node.CreatedAt)
	if err != nil {
		if clusterdb.IsConstraint(err) {
			return Node{}, fmt.Errorf("%w: cluster node already exists", failure.ErrConflict)
		}

		return Node{}, fmt.Errorf("create cluster node: %w", err)
	}

	return node, nil
}

// ListNodes returns cluster nodes ordered by creation time.
func (s *Service) ListNodes(ctx context.Context, limit int, cursor string) (services.Page[Node], error) {
	return listResources(ctx, s.db, `SELECT id, key, url, created_at FROM cluster_nodes`, limit, cursor, scanNodes, func(node Node) string { return node.CreatedAt }, "cluster nodes")
}

// GetNode returns a cluster node by ID or key.
func (s *Service) GetNode(ctx context.Context, nodeID, key string) (Node, error) {
	return getResource(ctx, s.db, `SELECT id, key, url, created_at FROM cluster_nodes WHERE `, nodeID, key, scanNode, "cluster node", "get cluster node")
}

// RemoveNode removes a cluster node by ID or key and returns the removed node.
func (s *Service) RemoveNode(ctx context.Context, nodeID, key string) (Node, error) {
	node, err := s.GetNode(ctx, nodeID, key)
	if err != nil {
		return Node{}, err
	}

	if _, err := s.db.Exec(ctx, `DELETE FROM cluster_nodes WHERE id = $1`, node.ID); err != nil {
		return Node{}, fmt.Errorf("remove cluster node: %w", err)
	}

	return node, nil
}

// CreateNamespace stores a cluster namespace.
func (s *Service) CreateNamespace(ctx context.Context, req CreateNamespaceRequest) (Namespace, error) {
	if err := validateOptionalKey("cluster namespace", namespaceIDPrefix, req.Key); err != nil {
		return Namespace{}, err
	}

	namespaceID, err := services.GenerateID(namespaceIDPrefix)
	if err != nil {
		return Namespace{}, err
	}

	namespace := Namespace{ID: namespaceID, Key: services.CopyStringPtr(req.Key), CreatedAt: services.Now()}

	_, err = s.db.Exec(ctx, `INSERT INTO cluster_namespaces (id, key, created_at) VALUES ($1, $2, $3)`, namespace.ID, services.OptionalStringValue(namespace.Key), namespace.CreatedAt)
	if err != nil {
		if clusterdb.IsConstraint(err) {
			return Namespace{}, fmt.Errorf("%w: cluster namespace already exists", failure.ErrConflict)
		}

		return Namespace{}, fmt.Errorf("create cluster namespace: %w", err)
	}

	return namespace, nil
}

// ListNamespaces returns cluster namespaces ordered by creation time.
func (s *Service) ListNamespaces(ctx context.Context, limit int, cursor string) (services.Page[Namespace], error) {
	return listResources(ctx, s.db, `SELECT id, key, created_at FROM cluster_namespaces`, limit, cursor, scanNamespaces, func(namespace Namespace) string { return namespace.CreatedAt }, "cluster namespaces")
}

// GetNamespace returns a cluster namespace by ID or key.
func (s *Service) GetNamespace(ctx context.Context, namespaceID, key string) (Namespace, error) {
	return getResource(ctx, s.db, `SELECT id, key, created_at FROM cluster_namespaces WHERE `, namespaceID, key, scanNamespace, "cluster namespace", "get cluster namespace")
}

// RemoveNamespace removes a cluster namespace by ID or key and returns the removed namespace.
func (s *Service) RemoveNamespace(ctx context.Context, namespaceID, key string) (Namespace, error) {
	namespace, err := s.GetNamespace(ctx, namespaceID, key)
	if err != nil {
		return Namespace{}, err
	}

	if _, err := s.db.Exec(ctx, `DELETE FROM cluster_namespaces WHERE id = $1`, namespace.ID); err != nil {
		return Namespace{}, fmt.Errorf("remove cluster namespace: %w", err)
	}

	return namespace, nil
}

// Health returns aggregate cluster health.
func (s *Service) Health(ctx context.Context) (Health, error) {
	nodes, err := s.allNodes(ctx)
	if err != nil {
		return Health{}, err
	}

	for _, node := range nodes {
		if err := s.nodeClient.Health(ctx, node.URL); err != nil {
			return Health{}, fmt.Errorf("%w: cluster node %s health check failed: %w", failure.ErrFailedDependency, node.ID, err)
		}
	}

	return Health{Status: "ok"}, nil
}

// Utilization returns aggregate utilization across all cluster nodes.
func (s *Service) Utilization(ctx context.Context) (utilization.Utilization, error) {
	nodes, err := s.allNodes(ctx)
	if err != nil {
		return utilization.Utilization{}, err
	}

	var aggregate utilization.Utilization

	for _, node := range nodes {
		nodeUtilization, err := s.nodeClient.Utilization(ctx, node.URL)
		if err != nil {
			return utilization.Utilization{}, fmt.Errorf("%w: cluster node %s utilization failed: %w", failure.ErrFailedDependency, node.ID, err)
		}

		aggregate.VCPU = addResource(aggregate.VCPU, nodeUtilization.VCPU)
		aggregate.Memory = addResource(aggregate.Memory, nodeUtilization.Memory)
		aggregate.Volume = addResource(aggregate.Volume, nodeUtilization.Volume)
	}

	return aggregate, nil
}

func (s *Service) allNodes(ctx context.Context) ([]Node, error) {
	rows, err := s.db.Query(ctx, `SELECT id, key, url, created_at FROM cluster_nodes ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list cluster nodes for aggregate: %w", err)
	}
	defer rows.Close()

	return scanNodes(rows, 0)
}

func validateOptionalKey(resource, prefix string, key *string) error {
	if err := services.ValidateOptionalKey(resource, key); err != nil {
		return err
	}

	if key != nil && strings.HasPrefix(*key, prefix+"_") {
		return fmt.Errorf("%w: %s key cannot use reserved %s_ prefix", failure.ErrInvalid, resource, prefix)
	}

	return nil
}

func validateNodeURL(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: cluster node url is required", failure.ErrInvalid)
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%w: cluster node url must be an absolute http or https URL", failure.ErrInvalid)
	}

	return nil
}

func queryPage(ctx context.Context, db *clusterdb.Client, query string, limit int, cursor string) (pgx.Rows, error) {
	if cursor == "" {
		return db.Query(ctx, query+` ORDER BY created_at LIMIT $1`, limit+1)
	}

	return db.Query(ctx, query+` WHERE created_at > $1 ORDER BY created_at LIMIT $2`, cursor, limit+1)
}

func listResources[T any](ctx context.Context, db *clusterdb.Client, query string, limit int, cursor string, scan func(pgx.Rows, int) ([]T, error), cursorValue func(T) string, label string) (services.Page[T], error) {
	limit = services.NormalizeLimit(limit)

	rows, err := queryPage(ctx, db, query, limit, cursor)
	if err != nil {
		return services.Page[T]{}, fmt.Errorf("list %s: %w", label, err)
	}
	defer rows.Close()

	entries, err := scan(rows, limit+1)
	if err != nil {
		return services.Page[T]{}, err
	}

	return services.FromEntries(entries, limit, cursorValue), nil
}

func getResource[T any](ctx context.Context, db *clusterdb.Client, query, id, key string, scan func(scanner) (T, error), label, operation string) (T, error) {
	var zero T

	if err := services.RequireIDOrKey(id, key); err != nil {
		return zero, err
	}

	where, value := lookupClause(id, key)

	resource, err := scan(db.QueryRow(ctx, query+where, value))
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, fmt.Errorf("%w: %s not found", failure.ErrNotFound, label)
	}

	if err != nil {
		return zero, fmt.Errorf("%s: %w", operation, err)
	}

	return resource, nil
}

func lookupClause(id, key string) (string, any) {
	if id != "" {
		return "id = $1", id
	}

	return "key = $1", key
}

type scanner interface {
	Scan(...any) error
}

func scanNode(row scanner) (Node, error) {
	var (
		node Node
		key  sql.NullString
	)
	if err := row.Scan(&node.ID, &key, &node.URL, &node.CreatedAt); err != nil {
		return Node{}, err
	}

	node.Key = services.NullStringPtr(key)

	return node, nil
}

func scanNodes(rows pgx.Rows, capacity int) ([]Node, error) {
	nodes := make([]Node, 0, capacity)

	for rows.Next() {
		node, err := scanNode(rows)
		if err != nil {
			return nil, fmt.Errorf("scan cluster node: %w", err)
		}

		nodes = append(nodes, node)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cluster nodes: %w", err)
	}

	return nodes, nil
}

func scanNamespace(row scanner) (Namespace, error) {
	var (
		namespace Namespace
		key       sql.NullString
	)
	if err := row.Scan(&namespace.ID, &key, &namespace.CreatedAt); err != nil {
		return Namespace{}, err
	}

	namespace.Key = services.NullStringPtr(key)

	return namespace, nil
}

func scanNamespaces(rows pgx.Rows, capacity int) ([]Namespace, error) {
	namespaces := make([]Namespace, 0, capacity)

	for rows.Next() {
		namespace, err := scanNamespace(rows)
		if err != nil {
			return nil, fmt.Errorf("scan cluster namespace: %w", err)
		}

		namespaces = append(namespaces, namespace)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cluster namespaces: %w", err)
	}

	return namespaces, nil
}

func addResource(left, right utilization.Resource) utilization.Resource {
	return utilization.Resource{
		Total:     left.Total + right.Total,
		Used:      left.Used + right.Used,
		Available: left.Available + right.Available,
	}
}

// HTTPNodeClient calls underlying nodes over HTTP.
type HTTPNodeClient struct {
	Client *http.Client
}

// Health verifies one underlying node health endpoint.
func (c HTTPNodeClient) Health(ctx context.Context, nodeURL string) error {
	var health Health
	if err := c.getJSON(ctx, nodeURL, "/v1/health", &health); err != nil {
		return err
	}

	if health.Status != "ok" {
		return fmt.Errorf("node status is %q", health.Status)
	}

	return nil
}

// Utilization returns one underlying node utilization response.
func (c HTTPNodeClient) Utilization(ctx context.Context, nodeURL string) (utilization.Utilization, error) {
	var out utilization.Utilization
	return out, c.getJSON(ctx, nodeURL, "/v1/utilization", &out)
}

// GetBase returns base metadata from one underlying node.
func (c HTTPNodeClient) GetBase(ctx context.Context, nodeURL string) (base.Base, error) {
	var out base.Base
	return out, c.getJSON(ctx, nodeURL, "/v1/base", &out)
}

// BuildBase builds the base on one underlying node.
func (c HTTPNodeClient) BuildBase(ctx context.Context, nodeURL string, req base.BuildRequest) (base.Base, error) {
	path := "/v1/base/build"
	if req.Force {
		path += "?force=true"
	}

	return c.postBaseStream(ctx, nodeURL, path, req, req.Logs)
}

// ImportBase imports a base archive into one underlying node.
func (c HTTPNodeClient) ImportBase(ctx context.Context, nodeURL string, req base.ImportRequest) (base.Base, error) {
	if req.Archive == nil {
		return base.Base{}, errors.New("base archive reader is required")
	}

	path := "/v1/base/import"
	if req.Force {
		path += "?force=true"
	}

	return c.postBaseStreamBody(ctx, nodeURL, path, req.Archive, req.ArchiveSize, req.Logs)
}

// ExportBase exports a base archive from one underlying node.
func (c HTTPNodeClient) ExportBase(ctx context.Context, nodeURL string, archive io.Writer) error {
	if archive == nil {
		return errors.New("base archive writer is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(nodeURL, "/")+"/v1/base/export", nil)
	if err != nil {
		return fmt.Errorf("create node request: %w", err)
	}

	req.Header.Set("Accept", base.ArchiveContentType)

	res, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("call node API: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return decodeNodeStatusError(res)
	}

	if _, err := io.Copy(archive, res.Body); err != nil {
		return fmt.Errorf("read node base archive: %w", err)
	}

	return nil
}

// CreateSecret creates a derivative secret on one underlying node.
func (c HTTPNodeClient) CreateSecret(ctx context.Context, nodeURL string, req secret.CreateRequest) (secret.Metadata, error) {
	var out secret.Metadata
	return out, c.doJSON(ctx, http.MethodPost, nodeURL, "/v1/secrets", req, &out)
}

// RemoveSecret removes a derivative secret from one underlying node.
func (c HTTPNodeClient) RemoveSecret(ctx context.Context, nodeURL, secretID string) error {
	return c.doJSON(ctx, http.MethodDelete, nodeURL, "/v1/secrets/"+url.PathEscape(secretID), nil, nil)
}

// CreateTemplate creates a derivative template on one underlying node.
func (c HTTPNodeClient) CreateTemplate(ctx context.Context, nodeURL string, req template.CreateRequest) (template.Metadata, error) {
	return c.postStream(ctx, nodeURL, "/v1/templates", req, req.Logs)
}

// ExportTemplate exports a derivative template from one underlying node.
func (c HTTPNodeClient) ExportTemplate(ctx context.Context, nodeURL, templateID string, archive io.Writer) error {
	if archive == nil {
		return errors.New("template archive writer is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(nodeURL, "/")+"/v1/templates/"+url.PathEscape(templateID)+"/export", nil)
	if err != nil {
		return fmt.Errorf("create node request: %w", err)
	}

	req.Header.Set("Accept", template.ArchiveContentType)

	res, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("call node API: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return decodeNodeStatusError(res)
	}

	if _, err := io.Copy(archive, res.Body); err != nil {
		return fmt.Errorf("read node template archive: %w", err)
	}

	return nil
}

// ImportTemplate imports a prepared derivative template archive into one underlying node.
func (c HTTPNodeClient) ImportTemplate(ctx context.Context, nodeURL string, req template.ImportRequest) (template.Metadata, error) {
	var out template.Metadata
	if req.Archive == nil {
		return out, errors.New("template archive reader is required")
	}

	target := strings.TrimRight(nodeURL, "/") + "/v1/templates/import"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, req.Archive)
	if err != nil {
		return out, fmt.Errorf("create node request: %w", err)
	}

	httpReq.Header.Set("Content-Type", template.ArchiveContentType)

	if req.ArchiveSize > 0 {
		httpReq.ContentLength = req.ArchiveSize
	}

	res, err := c.httpClient().Do(httpReq)
	if err != nil {
		return out, fmt.Errorf("call node API: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return out, decodeNodeStatusError(res)
	}

	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return out, fmt.Errorf("decode node response: %w", err)
	}

	return out, nil
}

// RemoveTemplate removes a derivative template from one underlying node.
func (c HTTPNodeClient) RemoveTemplate(ctx context.Context, nodeURL, templateID string) error {
	return c.doJSON(ctx, http.MethodDelete, nodeURL, "/v1/templates/"+url.PathEscape(templateID), nil, nil)
}

// CreateEnvironment creates a derivative environment on one underlying node.
func (c HTTPNodeClient) CreateEnvironment(ctx context.Context, nodeURL string, req environment.CreateRequest) (environment.Environment, error) {
	return c.postEnvironmentStream(ctx, nodeURL, "/v1/environments", req, req.Logs)
}

// GetEnvironment returns a derivative environment from one underlying node.
func (c HTTPNodeClient) GetEnvironment(ctx context.Context, nodeURL, environmentID string) (environment.Environment, error) {
	var out environment.Environment
	return out, c.doJSON(ctx, http.MethodGet, nodeURL, "/v1/environments/"+url.PathEscape(environmentID), nil, &out)
}

// RemoveEnvironment removes a derivative environment from one underlying node.
func (c HTTPNodeClient) RemoveEnvironment(ctx context.Context, nodeURL, environmentID string) (environment.Environment, error) {
	var out environment.Environment
	return out, c.doJSON(ctx, http.MethodDelete, nodeURL, "/v1/environments/"+url.PathEscape(environmentID), nil, &out)
}

// OpenSSH opens an upgraded SSH stream to a derivative environment on one underlying node.
func (c HTTPNodeClient) OpenSSH(ctx context.Context, nodeURL, environmentID string, tunnelReq sshtunnel.Request) (io.ReadWriteCloser, error) {
	contents, err := json.Marshal(tunnelReq)
	if err != nil {
		return nil, fmt.Errorf("encode node request: %w", err)
	}

	target, err := url.Parse(strings.TrimRight(nodeURL, "/") + "/v1/environments/" + url.PathEscape(environmentID) + "/ssh")
	if err != nil {
		return nil, fmt.Errorf("parse node API URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target.String(), bytes.NewReader(contents))
	if err != nil {
		return nil, fmt.Errorf("create node request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", sshtunnel.Protocol)

	conn, err := dialNodeHTTP(ctx, target)
	if err != nil {
		return nil, err
	}

	if err := req.Write(conn); err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("write node SSH upgrade request: %w", err)
	}

	reader := bufio.NewReader(conn)

	res, err := http.ReadResponse(reader, req)
	if err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("read node SSH upgrade response: %w", err)
	}

	if res.StatusCode != http.StatusSwitchingProtocols {
		defer func() { _ = res.Body.Close() }()
		defer func() { _ = conn.Close() }()

		if res.StatusCode >= http.StatusBadRequest {
			return nil, decodeNodeStatusError(res)
		}

		return nil, fmt.Errorf("node API returned %s, want %d Switching Protocols", res.Status, http.StatusSwitchingProtocols)
	}

	if !strings.EqualFold(res.Header.Get("Upgrade"), sshtunnel.Protocol) {
		_ = conn.Close()

		return nil, fmt.Errorf("node API returned unexpected SSH upgrade protocol %q", res.Header.Get("Upgrade"))
	}

	return &nodeUpgradedConn{Conn: conn, reader: reader}, nil
}

func (c HTTPNodeClient) getJSON(ctx context.Context, nodeURL, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(nodeURL, "/")+path, nil)
	if err != nil {
		return fmt.Errorf("create node request: %w", err)
	}

	res, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("call node API: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("node API returned %s", res.Status)
	}

	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode node response: %w", err)
	}

	return nil
}

func (c HTTPNodeClient) doJSON(ctx context.Context, method, nodeURL, path string, in, out any) error {
	var body io.Reader

	if in != nil {
		contents, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("encode node request: %w", err)
		}

		body = bytes.NewReader(contents)
	}

	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(nodeURL, "/")+path, body)
	if err != nil {
		return fmt.Errorf("create node request: %w", err)
	}

	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("call node API: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return decodeNodeStatusError(res)
	}

	if out == nil {
		return nil
	}

	if err := json.NewDecoder(res.Body).Decode(out); err != nil {
		return fmt.Errorf("decode node response: %w", err)
	}

	return nil
}

//nolint:dupl // Mirrors template/environment stream handling while preserving base-specific result typing.
func (c HTTPNodeClient) postBaseStream(ctx context.Context, nodeURL, path string, in any, logs io.Writer) (base.Base, error) {
	var out base.Base

	contents, err := json.Marshal(in)
	if err != nil {
		return out, fmt.Errorf("encode node request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(nodeURL, "/")+path, bytes.NewReader(contents))
	if err != nil {
		return out, fmt.Errorf("create node request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")

	res, err := c.httpClient().Do(req)
	if err != nil {
		return out, fmt.Errorf("call node API: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return out, decodeNodeStatusError(res)
	}

	decoder := json.NewDecoder(res.Body)
	for {
		created, done, err := readBaseStreamEvent(decoder, logs)
		if err != nil {
			return out, err
		}

		if done {
			return created, nil
		}
	}
}

func (c HTTPNodeClient) postBaseStreamBody(ctx context.Context, nodeURL, path string, body io.Reader, contentLength int64, logs io.Writer) (base.Base, error) {
	var out base.Base

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(nodeURL, "/")+path, body)
	if err != nil {
		return out, fmt.Errorf("create node request: %w", err)
	}

	req.Header.Set("Content-Type", base.ArchiveContentType)
	req.Header.Set("Accept", "application/x-ndjson")

	if contentLength > 0 {
		req.ContentLength = contentLength
	}

	res, err := c.httpClient().Do(req)
	if err != nil {
		return out, fmt.Errorf("call node API: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return out, decodeNodeStatusError(res)
	}

	decoder := json.NewDecoder(res.Body)
	for {
		created, done, err := readBaseStreamEvent(decoder, logs)
		if err != nil {
			return out, err
		}

		if done {
			return created, nil
		}
	}
}

//nolint:dupl // Template and environment streams carry distinct event/result types.
func (c HTTPNodeClient) postStream(ctx context.Context, nodeURL, path string, in any, logs io.Writer) (template.Metadata, error) {
	var out template.Metadata

	contents, err := json.Marshal(in)
	if err != nil {
		return out, fmt.Errorf("encode node request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(nodeURL, "/")+path, bytes.NewReader(contents))
	if err != nil {
		return out, fmt.Errorf("create node request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")

	res, err := c.httpClient().Do(req)
	if err != nil {
		return out, fmt.Errorf("call node API: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return out, decodeNodeStatusError(res)
	}

	decoder := json.NewDecoder(res.Body)

	for {
		created, done, err := readTemplateCreateStreamEvent(decoder, logs)
		if err != nil {
			return out, err
		}

		if done {
			return created, nil
		}
	}
}

//nolint:dupl // Mirrors template stream handling while preserving environment-specific result typing.
func (c HTTPNodeClient) postEnvironmentStream(ctx context.Context, nodeURL, path string, in any, logs io.Writer) (environment.Environment, error) {
	var out environment.Environment

	contents, err := json.Marshal(in)
	if err != nil {
		return out, fmt.Errorf("encode node request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(nodeURL, "/")+path, bytes.NewReader(contents))
	if err != nil {
		return out, fmt.Errorf("create node request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")

	res, err := c.httpClient().Do(req)
	if err != nil {
		return out, fmt.Errorf("call node API: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode >= http.StatusBadRequest {
		return out, decodeNodeStatusError(res)
	}

	decoder := json.NewDecoder(res.Body)
	for {
		created, done, err := readEnvironmentCreateStreamEvent(decoder, logs)
		if err != nil {
			return out, err
		}

		if done {
			return created, nil
		}
	}
}

//nolint:dupl // Stream event shape intentionally matches template creation with different payload types.
func readEnvironmentCreateStreamEvent(decoder *json.Decoder, logs io.Writer) (environment.Environment, bool, error) {
	var event environment.CreateStreamEvent

	if err := decoder.Decode(&event); err != nil {
		if errors.Is(err, io.EOF) {
			return environment.Environment{}, false, errors.New("node API stream ended before environment creation completed")
		}

		return environment.Environment{}, false, fmt.Errorf("decode node environment stream: %w", err)
	}

	switch event.Type {
	case environment.StreamEventLog:
		if logs != nil && event.Log != "" {
			if _, err := logs.Write([]byte(event.Log)); err != nil {
				return environment.Environment{}, false, fmt.Errorf("stream node environment logs: %w", err)
			}
		}

		return environment.Environment{}, false, nil
	case environment.StreamEventResult:
		if event.Environment == nil {
			return environment.Environment{}, false, errors.New("node API stream result missing environment")
		}

		return *event.Environment, true, nil
	case environment.StreamEventError:
		return environment.Environment{}, false, fmt.Errorf("node API environment creation failed: %s", event.Error)
	default:
		return environment.Environment{}, false, fmt.Errorf("node API environment stream returned unknown event type %q", event.Type)
	}
}

//nolint:dupl // Stream event shape intentionally matches environment creation with different payload types.
func readTemplateCreateStreamEvent(decoder *json.Decoder, logs io.Writer) (template.Metadata, bool, error) {
	var event template.CreateStreamEvent

	if err := decoder.Decode(&event); err != nil {
		if errors.Is(err, io.EOF) {
			return template.Metadata{}, false, errors.New("node API stream ended before template creation completed")
		}

		return template.Metadata{}, false, fmt.Errorf("decode node template stream: %w", err)
	}

	switch event.Type {
	case template.StreamEventLog:
		if logs != nil && event.Log != "" {
			if _, err := logs.Write([]byte(event.Log)); err != nil {
				return template.Metadata{}, false, fmt.Errorf("stream node template logs: %w", err)
			}
		}

		return template.Metadata{}, false, nil
	case template.StreamEventResult:
		if event.Template == nil {
			return template.Metadata{}, false, errors.New("node API stream result missing template")
		}

		return *event.Template, true, nil
	case template.StreamEventError:
		return template.Metadata{}, false, fmt.Errorf("node API template creation failed: %s", event.Error)
	default:
		return template.Metadata{}, false, fmt.Errorf("node API template stream returned unknown event type %q", event.Type)
	}
}

//nolint:dupl // Stream event shape intentionally matches template/environment creation with a base payload.
func readBaseStreamEvent(decoder *json.Decoder, logs io.Writer) (base.Base, bool, error) {
	var event base.StreamEvent

	if err := decoder.Decode(&event); err != nil {
		if errors.Is(err, io.EOF) {
			return base.Base{}, false, errors.New("node API stream ended before base operation completed")
		}

		return base.Base{}, false, fmt.Errorf("decode node base stream: %w", err)
	}

	switch event.Type {
	case base.StreamEventLog:
		if logs != nil && event.Log != "" {
			if _, err := logs.Write([]byte(event.Log)); err != nil {
				return base.Base{}, false, fmt.Errorf("stream node base logs: %w", err)
			}
		}

		return base.Base{}, false, nil
	case base.StreamEventResult:
		if event.Base == nil {
			return base.Base{}, false, errors.New("node API stream result missing base")
		}

		return *event.Base, true, nil
	case base.StreamEventError:
		return base.Base{}, false, fmt.Errorf("node API base operation failed: %s", event.Error)
	default:
		return base.Base{}, false, fmt.Errorf("node API base stream returned unknown event type %q", event.Type)
	}
}

func (c HTTPNodeClient) httpClient() *http.Client {
	if c.Client != nil {
		return c.Client
	}

	return &http.Client{Timeout: nodeClientTimeout}
}

func decodeNodeStatusError(res *http.Response) error {
	var apiErr struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&apiErr); err != nil || apiErr.Error == "" {
		return wrapNodeStatusError(res.StatusCode, fmt.Errorf("node API returned %s", res.Status))
	}

	return wrapNodeStatusError(res.StatusCode, fmt.Errorf("node API returned %s: %s", res.Status, apiErr.Error))
}

func wrapNodeStatusError(status int, err error) error {
	switch status {
	case http.StatusBadRequest:
		return fmt.Errorf("%w: %w", failure.ErrInvalid, err)
	case http.StatusNotFound:
		return fmt.Errorf("%w: %w", failure.ErrNotFound, err)
	case http.StatusConflict:
		return fmt.Errorf("%w: %w", failure.ErrConflict, err)
	case http.StatusFailedDependency:
		return fmt.Errorf("%w: %w", failure.ErrFailedDependency, err)
	default:
		return err
	}
}

func dialNodeHTTP(ctx context.Context, target *url.URL) (net.Conn, error) {
	host := target.Hostname()
	port := target.Port()

	if port == "" {
		switch target.Scheme {
		case "http":
			port = "80"
		case "https":
			port = "443"
		}
	}

	addr := net.JoinHostPort(host, port)
	dialer := net.Dialer{}

	switch target.Scheme {
	case "http":
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("dial node API: %w", err)
		}

		return conn, nil
	case "https":
		tlsDialer := tls.Dialer{NetDialer: &dialer, Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}}

		conn, err := tlsDialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("dial node API: %w", err)
		}

		return conn, nil
	default:
		return nil, fmt.Errorf("unsupported node API scheme %q", target.Scheme)
	}
}

type nodeUpgradedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *nodeUpgradedConn) Read(p []byte) (int, error) {
	if c.reader.Buffered() > 0 {
		return c.reader.Read(p)
	}

	return c.Conn.Read(p)
}
