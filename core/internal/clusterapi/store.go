//nolint:dupl,wsl_v5 // Memory store CRUD methods intentionally mirror each resource type.
package clusterapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/schema"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/cluster"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/secret"
	"github.com/bastion-computer/bastion/core/internal/services/template"
)

const (
	nodeIDPrefix      = "node"
	namespaceIDPrefix = "ns"
)

// Store persists cluster control plane state.
type Store interface {
	CreateNode(context.Context, cluster.CreateNodeRequest) (cluster.Node, error)
	ListNodes(context.Context, int, string) (services.Page[cluster.Node], error)
	GetNode(context.Context, string, string) (cluster.Node, error)
	RemoveNode(context.Context, string, string) (cluster.Node, error)
	CreateNamespace(context.Context, cluster.CreateNamespaceRequest) (cluster.Namespace, error)
	ListNamespaces(context.Context, int, string) (services.Page[cluster.Namespace], error)
	GetNamespace(context.Context, string, string) (cluster.Namespace, error)
	RemoveNamespace(context.Context, string, string) (cluster.Namespace, error)
	ResolveNamespace(context.Context, string) (cluster.Namespace, error)
	CreateSecret(context.Context, string, secret.CreateRequest) (secret.Metadata, error)
	ListSecrets(context.Context, string, int, string) (services.Page[secret.Metadata], error)
	GetSecret(context.Context, string, string, string) (secret.Secret, error)
	RemoveSecret(context.Context, string, string, string) (secret.Metadata, error)
	CreateTemplate(context.Context, string, template.Template, string) (template.Metadata, error)
	ListTemplates(context.Context, string, int, string) (services.Page[template.Metadata], error)
	GetTemplate(context.Context, string, string, string) (StoredTemplate, error)
	RemoveTemplate(context.Context, string, string, string) (StoredTemplate, error)
	TemplateDerivative(context.Context, string, string) (string, bool, error)
	SaveTemplateDerivative(context.Context, string, string, string) error
	RemoveTemplateDerivative(context.Context, string, string) error
	CreateEnvironment(context.Context, string, EnvironmentRecord) (environment.Environment, error)
	ListEnvironments(context.Context, string, int, string, []string) (services.Page[environment.Environment], error)
	GetEnvironment(context.Context, string, string, string) (EnvironmentRecord, error)
	RemoveEnvironment(context.Context, string, string, string) (EnvironmentRecord, error)
}

// StoredTemplate includes cluster-private archive metadata for a source template.
type StoredTemplate struct {
	template.Template
	ArchiveKey string
}

// EnvironmentRecord includes cluster-private placement metadata for a source environment.
type EnvironmentRecord struct {
	environment.Environment
	NodeID                  string
	DerivativeTemplateID    string
	DerivativeEnvironmentID string
}

// MemoryStore is an in-memory cluster Store used by tests and lightweight local checks.
type MemoryStore struct {
	mu           sync.Mutex
	nodes        map[string]cluster.Node
	nodeKeys     map[string]string
	namespaces   map[string]cluster.Namespace
	nsKeys       map[string]string
	secrets      map[string]map[string]secret.Secret
	secretKeys   map[string]map[string]string
	templates    map[string]map[string]StoredTemplate
	tplKeys      map[string]map[string]string
	derivatives  map[string]map[string]string
	environments map[string]map[string]EnvironmentRecord
	envKeys      map[string]map[string]string
}

// NewMemoryStore returns an empty in-memory Store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		nodes:        make(map[string]cluster.Node),
		nodeKeys:     make(map[string]string),
		namespaces:   make(map[string]cluster.Namespace),
		nsKeys:       make(map[string]string),
		secrets:      make(map[string]map[string]secret.Secret),
		secretKeys:   make(map[string]map[string]string),
		templates:    make(map[string]map[string]StoredTemplate),
		tplKeys:      make(map[string]map[string]string),
		derivatives:  make(map[string]map[string]string),
		environments: make(map[string]map[string]EnvironmentRecord),
		envKeys:      make(map[string]map[string]string),
	}
}

// CreateNode stores a cluster node.
func (s *MemoryStore) CreateNode(_ context.Context, req cluster.CreateNodeRequest) (cluster.Node, error) {
	if err := validateNodeRequest(req); err != nil {
		return cluster.Node{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if req.Key != nil {
		if _, ok := s.nodeKeys[*req.Key]; ok {
			return cluster.Node{}, fmt.Errorf("%w: node already exists", failure.ErrConflict)
		}
	}

	id, err := services.GenerateID(nodeIDPrefix)
	if err != nil {
		return cluster.Node{}, err
	}

	node := cluster.Node{ID: id, Key: services.CopyStringPtr(req.Key), APIURL: strings.TrimRight(req.APIURL, "/"), CreatedAt: services.Now()}
	s.nodes[node.ID] = node
	if node.Key != nil {
		s.nodeKeys[*node.Key] = node.ID
	}

	return node, nil
}

// ListNodes returns registered nodes ordered by creation time.
func (s *MemoryStore) ListNodes(_ context.Context, limit int, cursor string) (services.Page[cluster.Node], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	limit = services.NormalizeLimit(limit)
	entries := make([]cluster.Node, 0, len(s.nodes))
	for _, node := range s.nodes {
		if cursor == "" || node.CreatedAt > cursor {
			entries = append(entries, copyNode(node))
		}
	}

	slices.SortFunc(entries, func(a, b cluster.Node) int { return strings.Compare(a.CreatedAt, b.CreatedAt) })

	return services.FromEntries(entries, limit, func(node cluster.Node) string { return node.CreatedAt }), nil
}

// GetNode returns a node by ID or key.
func (s *MemoryStore) GetNode(_ context.Context, id, key string) (cluster.Node, error) {
	if err := services.RequireIDOrKey(id, key); err != nil {
		return cluster.Node{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	node, err := s.getNodeLocked(id, key)
	if err != nil {
		return cluster.Node{}, err
	}

	return copyNode(node), nil
}

// RemoveNode removes a node by ID or key.
func (s *MemoryStore) RemoveNode(_ context.Context, id, key string) (cluster.Node, error) {
	if err := services.RequireIDOrKey(id, key); err != nil {
		return cluster.Node{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	node, err := s.getNodeLocked(id, key)
	if err != nil {
		return cluster.Node{}, err
	}

	delete(s.nodes, node.ID)
	if node.Key != nil {
		delete(s.nodeKeys, *node.Key)
	}

	return copyNode(node), nil
}

func (s *MemoryStore) getNodeLocked(id, key string) (cluster.Node, error) {
	if key != "" {
		var ok bool
		id, ok = s.nodeKeys[key]
		if !ok {
			return cluster.Node{}, fmt.Errorf("%w: node not found", failure.ErrNotFound)
		}
	}

	node, ok := s.nodes[id]
	if !ok {
		return cluster.Node{}, fmt.Errorf("%w: node not found", failure.ErrNotFound)
	}

	return node, nil
}

// CreateNamespace stores a tenant namespace.
func (s *MemoryStore) CreateNamespace(_ context.Context, req cluster.CreateNamespaceRequest) (cluster.Namespace, error) {
	if err := validateNamespaceRequest(req); err != nil {
		return cluster.Namespace{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if req.Key != nil {
		if _, ok := s.nsKeys[*req.Key]; ok {
			return cluster.Namespace{}, fmt.Errorf("%w: namespace already exists", failure.ErrConflict)
		}
	}

	id, err := services.GenerateID(namespaceIDPrefix)
	if err != nil {
		return cluster.Namespace{}, err
	}

	namespace := cluster.Namespace{ID: id, Key: services.CopyStringPtr(req.Key), Limits: req.Limits, CreatedAt: services.Now()}
	s.namespaces[namespace.ID] = namespace
	if namespace.Key != nil {
		s.nsKeys[*namespace.Key] = namespace.ID
	}

	return namespace, nil
}

// ListNamespaces returns namespaces ordered by creation time.
func (s *MemoryStore) ListNamespaces(_ context.Context, limit int, cursor string) (services.Page[cluster.Namespace], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	limit = services.NormalizeLimit(limit)
	entries := make([]cluster.Namespace, 0, len(s.namespaces))
	for _, namespace := range s.namespaces {
		if cursor == "" || namespace.CreatedAt > cursor {
			entries = append(entries, copyNamespace(namespace))
		}
	}

	slices.SortFunc(entries, func(a, b cluster.Namespace) int { return strings.Compare(a.CreatedAt, b.CreatedAt) })

	return services.FromEntries(entries, limit, func(namespace cluster.Namespace) string { return namespace.CreatedAt }), nil
}

// GetNamespace returns a namespace by ID or key.
func (s *MemoryStore) GetNamespace(_ context.Context, id, key string) (cluster.Namespace, error) {
	if err := services.RequireIDOrKey(id, key); err != nil {
		return cluster.Namespace{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	namespace, err := s.getNamespaceLocked(id, key)
	if err != nil {
		return cluster.Namespace{}, err
	}

	return copyNamespace(namespace), nil
}

// RemoveNamespace removes a namespace by ID or key.
func (s *MemoryStore) RemoveNamespace(_ context.Context, id, key string) (cluster.Namespace, error) {
	if err := services.RequireIDOrKey(id, key); err != nil {
		return cluster.Namespace{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	namespace, err := s.getNamespaceLocked(id, key)
	if err != nil {
		return cluster.Namespace{}, err
	}

	delete(s.namespaces, namespace.ID)
	if namespace.Key != nil {
		delete(s.nsKeys, *namespace.Key)
	}

	return copyNamespace(namespace), nil
}

func (s *MemoryStore) getNamespaceLocked(id, key string) (cluster.Namespace, error) {
	if key != "" {
		var ok bool
		id, ok = s.nsKeys[key]
		if !ok {
			return cluster.Namespace{}, fmt.Errorf("%w: namespace not found", failure.ErrNotFound)
		}
	}

	namespace, ok := s.namespaces[id]
	if !ok {
		return cluster.Namespace{}, fmt.Errorf("%w: namespace not found", failure.ErrNotFound)
	}

	return namespace, nil
}

// ResolveNamespace resolves ns_ identifiers by ID and all other values by key.
func (s *MemoryStore) ResolveNamespace(ctx context.Context, reference string) (cluster.Namespace, error) {
	if strings.HasPrefix(reference, namespaceIDPrefix+"_") {
		return s.GetNamespace(ctx, reference, "")
	}

	return s.GetNamespace(ctx, "", reference)
}

// CreateSecret stores a source secret inside a namespace.
func (s *MemoryStore) CreateSecret(_ context.Context, namespaceID string, req secret.CreateRequest) (secret.Metadata, error) {
	if err := validateSecretRequest(req); err != nil {
		return secret.Metadata{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.namespaces[namespaceID]; !ok {
		return secret.Metadata{}, fmt.Errorf("%w: namespace not found", failure.ErrNotFound)
	}

	secrets, secretKeys := s.ensureSecretMaps(namespaceID)
	if req.Key != nil {
		if _, ok := secretKeys[*req.Key]; ok {
			return secret.Metadata{}, fmt.Errorf("%w: secret already exists", failure.ErrConflict)
		}
	}

	id, err := services.GenerateID("sec")
	if err != nil {
		return secret.Metadata{}, err
	}

	created := secret.Secret{ID: id, Key: services.CopyStringPtr(req.Key), Value: req.Value, CreatedAt: services.Now()}
	secrets[created.ID] = created
	if created.Key != nil {
		secretKeys[*created.Key] = created.ID
	}

	return created.Metadata(), nil
}

// ListSecrets returns source secret metadata in a namespace.
func (s *MemoryStore) ListSecrets(_ context.Context, namespaceID string, limit int, cursor string) (services.Page[secret.Metadata], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	limit = services.NormalizeLimit(limit)
	entries := make([]secret.Metadata, 0, len(s.secrets[namespaceID]))
	for _, stored := range s.secrets[namespaceID] {
		if cursor == "" || stored.CreatedAt > cursor {
			entries = append(entries, stored.Metadata())
		}
	}

	slices.SortFunc(entries, func(a, b secret.Metadata) int { return strings.Compare(a.CreatedAt, b.CreatedAt) })

	return services.FromEntries(entries, limit, func(entry secret.Metadata) string { return entry.CreatedAt }), nil
}

// GetSecret returns a source secret by ID or key.
func (s *MemoryStore) GetSecret(_ context.Context, namespaceID, id, key string) (secret.Secret, error) {
	if err := services.RequireIDOrKey(id, key); err != nil {
		return secret.Secret{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.getSecretLocked(namespaceID, id, key)
	if err != nil {
		return secret.Secret{}, err
	}

	return copySecret(stored), nil
}

// RemoveSecret deletes a source secret by ID or key.
func (s *MemoryStore) RemoveSecret(_ context.Context, namespaceID, id, key string) (secret.Metadata, error) {
	if err := services.RequireIDOrKey(id, key); err != nil {
		return secret.Metadata{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.getSecretLocked(namespaceID, id, key)
	if err != nil {
		return secret.Metadata{}, err
	}

	delete(s.secrets[namespaceID], stored.ID)
	if stored.Key != nil {
		delete(s.secretKeys[namespaceID], *stored.Key)
	}

	return stored.Metadata(), nil
}

// CreateTemplate stores a source template.
func (s *MemoryStore) CreateTemplate(_ context.Context, namespaceID string, source template.Template, archiveKey string) (template.Metadata, error) {
	if err := validateStoredTemplate(source, archiveKey); err != nil {
		return template.Metadata{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.namespaces[namespaceID]; !ok {
		return template.Metadata{}, fmt.Errorf("%w: namespace not found", failure.ErrNotFound)
	}

	templates, templateKeys := s.ensureTemplateMaps(namespaceID)
	if source.Key != nil {
		if _, ok := templateKeys[*source.Key]; ok {
			return template.Metadata{}, fmt.Errorf("%w: template already exists", failure.ErrConflict)
		}
	}

	stored := StoredTemplate{Template: copyTemplate(source), ArchiveKey: archiveKey}
	templates[stored.ID] = stored
	if stored.Key != nil {
		templateKeys[*stored.Key] = stored.ID
	}

	return stored.Metadata(), nil
}

// ListTemplates returns source template metadata in a namespace.
func (s *MemoryStore) ListTemplates(_ context.Context, namespaceID string, limit int, cursor string) (services.Page[template.Metadata], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	limit = services.NormalizeLimit(limit)
	entries := make([]template.Metadata, 0, len(s.templates[namespaceID]))
	for _, stored := range s.templates[namespaceID] {
		if cursor == "" || stored.CreatedAt > cursor {
			entries = append(entries, stored.Metadata())
		}
	}

	slices.SortFunc(entries, func(a, b template.Metadata) int { return strings.Compare(a.CreatedAt, b.CreatedAt) })

	return services.FromEntries(entries, limit, func(entry template.Metadata) string { return entry.CreatedAt }), nil
}

// GetTemplate returns a source template by ID or key.
func (s *MemoryStore) GetTemplate(_ context.Context, namespaceID, id, key string) (StoredTemplate, error) {
	if err := services.RequireIDOrKey(id, key); err != nil {
		return StoredTemplate{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.getTemplateLocked(namespaceID, id, key)
	if err != nil {
		return StoredTemplate{}, err
	}

	return copyStoredTemplate(stored), nil
}

// RemoveTemplate removes a source template if no source environments use it.
func (s *MemoryStore) RemoveTemplate(_ context.Context, namespaceID, id, key string) (StoredTemplate, error) {
	if err := services.RequireIDOrKey(id, key); err != nil {
		return StoredTemplate{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.getTemplateLocked(namespaceID, id, key)
	if err != nil {
		return StoredTemplate{}, err
	}

	for _, env := range s.environments[namespaceID] {
		if env.TemplateID == stored.ID {
			return StoredTemplate{}, fmt.Errorf("%w: template is in use", failure.ErrConflict)
		}
	}

	delete(s.templates[namespaceID], stored.ID)
	if stored.Key != nil {
		delete(s.tplKeys[namespaceID], *stored.Key)
	}
	delete(s.derivatives, stored.ID)

	return copyStoredTemplate(stored), nil
}

// TemplateDerivative returns a node-local derivative template ID.
func (s *MemoryStore) TemplateDerivative(_ context.Context, sourceTemplateID, nodeID string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	derivatives := s.derivatives[sourceTemplateID]
	if derivatives == nil {
		return "", false, nil
	}

	derivativeID, ok := derivatives[nodeID]

	return derivativeID, ok, nil
}

// SaveTemplateDerivative records a node-local derivative template ID.
func (s *MemoryStore) SaveTemplateDerivative(_ context.Context, sourceTemplateID, nodeID, derivativeTemplateID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	derivatives := s.derivatives[sourceTemplateID]
	if derivatives == nil {
		derivatives = make(map[string]string)
		s.derivatives[sourceTemplateID] = derivatives
	}

	derivatives[nodeID] = derivativeTemplateID

	return nil
}

// RemoveTemplateDerivative removes a node-local derivative template mapping.
func (s *MemoryStore) RemoveTemplateDerivative(_ context.Context, sourceTemplateID, nodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if derivatives := s.derivatives[sourceTemplateID]; derivatives != nil {
		delete(derivatives, nodeID)
	}

	return nil
}

// CreateEnvironment stores a source environment placement.
func (s *MemoryStore) CreateEnvironment(_ context.Context, namespaceID string, record EnvironmentRecord) (environment.Environment, error) {
	if err := validateEnvironmentRecord(record); err != nil {
		return environment.Environment{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.namespaces[namespaceID]; !ok {
		return environment.Environment{}, fmt.Errorf("%w: namespace not found", failure.ErrNotFound)
	}

	environments, environmentKeys := s.ensureEnvironmentMaps(namespaceID)
	if record.Key != nil {
		if _, ok := environmentKeys[*record.Key]; ok {
			return environment.Environment{}, fmt.Errorf("%w: environment already exists", failure.ErrConflict)
		}
	}

	stored := copyEnvironmentRecord(record)
	environments[stored.ID] = stored
	if stored.Key != nil {
		environmentKeys[*stored.Key] = stored.ID
	}

	return copyEnvironment(stored.Environment), nil
}

// ListEnvironments returns source environments in a namespace.
func (s *MemoryStore) ListEnvironments(_ context.Context, namespaceID string, limit int, cursor string, tags []string) (services.Page[environment.Environment], error) {
	limit = services.NormalizeLimit(limit)

	s.mu.Lock()
	defer s.mu.Unlock()

	entries := make([]environment.Environment, 0, len(s.environments[namespaceID]))
	for _, stored := range s.environments[namespaceID] {
		if cursor != "" && stored.CreatedAt <= cursor {
			continue
		}

		if !environmentHasTags(stored.Environment, tags) {
			continue
		}

		entries = append(entries, copyEnvironment(stored.Environment))
	}

	slices.SortFunc(entries, func(a, b environment.Environment) int { return strings.Compare(a.CreatedAt, b.CreatedAt) })

	return services.FromEntries(entries, limit, func(entry environment.Environment) string { return entry.CreatedAt }), nil
}

// GetEnvironment returns a source environment by ID or key.
func (s *MemoryStore) GetEnvironment(_ context.Context, namespaceID, id, key string) (EnvironmentRecord, error) {
	if err := services.RequireIDOrKey(id, key); err != nil {
		return EnvironmentRecord{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.getEnvironmentLocked(namespaceID, id, key)
	if err != nil {
		return EnvironmentRecord{}, err
	}

	return copyEnvironmentRecord(stored), nil
}

// RemoveEnvironment removes a source environment by ID or key.
func (s *MemoryStore) RemoveEnvironment(_ context.Context, namespaceID, id, key string) (EnvironmentRecord, error) {
	if err := services.RequireIDOrKey(id, key); err != nil {
		return EnvironmentRecord{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	stored, err := s.getEnvironmentLocked(namespaceID, id, key)
	if err != nil {
		return EnvironmentRecord{}, err
	}

	delete(s.environments[namespaceID], stored.ID)
	if stored.Key != nil {
		delete(s.envKeys[namespaceID], *stored.Key)
	}

	return copyEnvironmentRecord(stored), nil
}

func (s *MemoryStore) ensureSecretMaps(namespaceID string) (map[string]secret.Secret, map[string]string) {
	secrets := s.secrets[namespaceID]
	if secrets == nil {
		secrets = make(map[string]secret.Secret)
		s.secrets[namespaceID] = secrets
	}

	keys := s.secretKeys[namespaceID]
	if keys == nil {
		keys = make(map[string]string)
		s.secretKeys[namespaceID] = keys
	}

	return secrets, keys
}

func (s *MemoryStore) ensureTemplateMaps(namespaceID string) (map[string]StoredTemplate, map[string]string) {
	templates := s.templates[namespaceID]
	if templates == nil {
		templates = make(map[string]StoredTemplate)
		s.templates[namespaceID] = templates
	}

	keys := s.tplKeys[namespaceID]
	if keys == nil {
		keys = make(map[string]string)
		s.tplKeys[namespaceID] = keys
	}

	return templates, keys
}

func (s *MemoryStore) ensureEnvironmentMaps(namespaceID string) (map[string]EnvironmentRecord, map[string]string) {
	environments := s.environments[namespaceID]
	if environments == nil {
		environments = make(map[string]EnvironmentRecord)
		s.environments[namespaceID] = environments
	}

	keys := s.envKeys[namespaceID]
	if keys == nil {
		keys = make(map[string]string)
		s.envKeys[namespaceID] = keys
	}

	return environments, keys
}

func (s *MemoryStore) getSecretLocked(namespaceID, id, key string) (secret.Secret, error) {
	if key != "" {
		var ok bool
		id, ok = s.secretKeys[namespaceID][key]
		if !ok {
			return secret.Secret{}, fmt.Errorf("%w: secret not found", failure.ErrNotFound)
		}
	}

	stored, ok := s.secrets[namespaceID][id]
	if !ok {
		return secret.Secret{}, fmt.Errorf("%w: secret not found", failure.ErrNotFound)
	}

	return stored, nil
}

func (s *MemoryStore) getTemplateLocked(namespaceID, id, key string) (StoredTemplate, error) {
	if key != "" {
		var ok bool
		id, ok = s.tplKeys[namespaceID][key]
		if !ok {
			return StoredTemplate{}, fmt.Errorf("%w: template not found", failure.ErrNotFound)
		}
	}

	stored, ok := s.templates[namespaceID][id]
	if !ok {
		return StoredTemplate{}, fmt.Errorf("%w: template not found", failure.ErrNotFound)
	}

	return stored, nil
}

func (s *MemoryStore) getEnvironmentLocked(namespaceID, id, key string) (EnvironmentRecord, error) {
	if key != "" {
		var ok bool
		id, ok = s.envKeys[namespaceID][key]
		if !ok {
			return EnvironmentRecord{}, fmt.Errorf("%w: environment not found", failure.ErrNotFound)
		}
	}

	stored, ok := s.environments[namespaceID][id]
	if !ok {
		return EnvironmentRecord{}, fmt.Errorf("%w: environment not found", failure.ErrNotFound)
	}

	return stored, nil
}

func validateSecretRequest(req secret.CreateRequest) error {
	if err := services.ValidateOptionalKey("secret", req.Key); err != nil {
		return err
	}

	if req.Key != nil && strings.HasPrefix(*req.Key, "sec_") {
		return fmt.Errorf("%w: secret key cannot use reserved sec_ prefix", failure.ErrInvalid)
	}

	if req.Value == "" {
		return fmt.Errorf("%w: secret value is required", failure.ErrInvalid)
	}

	return nil
}

func validateStoredTemplate(source template.Template, archiveKey string) error {
	if source.ID == "" {
		return fmt.Errorf("%w: template id is required", failure.ErrInvalid)
	}

	if err := services.ValidateOptionalKey("template", source.Key); err != nil {
		return err
	}

	if len(source.Config) == 0 || !json.Valid(source.Config) {
		return fmt.Errorf("%w: template config must be valid JSON", failure.ErrInvalid)
	}

	if err := schema.ValidateTemplateConfig(source.Config); err != nil {
		return fmt.Errorf("%w: template config does not match schema: %w", failure.ErrInvalid, err)
	}

	if archiveKey == "" {
		return fmt.Errorf("%w: template archive key is required", failure.ErrInvalid)
	}

	return nil
}

func validateEnvironmentRecord(record EnvironmentRecord) error {
	if record.ID == "" || record.TemplateID == "" || record.NodeID == "" || record.DerivativeTemplateID == "" || record.DerivativeEnvironmentID == "" {
		return fmt.Errorf("%w: environment placement is incomplete", failure.ErrInvalid)
	}

	return services.ValidateOptionalKey("environment", record.Key)
}

func environmentHasTags(entry environment.Environment, tags []string) bool {
	for _, tag := range tags {
		if !slices.Contains(entry.Tags, tag) {
			return false
		}
	}

	return true
}

func copySecret(stored secret.Secret) secret.Secret {
	stored.Key = services.CopyStringPtr(stored.Key)

	return stored
}

func copyTemplate(stored template.Template) template.Template {
	stored.Key = services.CopyStringPtr(stored.Key)
	stored.Config = append(json.RawMessage(nil), stored.Config...)

	return stored
}

func copyStoredTemplate(stored StoredTemplate) StoredTemplate {
	stored.Template = copyTemplate(stored.Template)

	return stored
}

func copyEnvironment(stored environment.Environment) environment.Environment {
	stored.Key = services.CopyStringPtr(stored.Key)
	stored.Tags = append([]string(nil), stored.Tags...)

	return stored
}

func copyEnvironmentRecord(stored EnvironmentRecord) EnvironmentRecord {
	stored.Environment = copyEnvironment(stored.Environment)

	return stored
}

func validateNodeRequest(req cluster.CreateNodeRequest) error {
	if err := services.ValidateOptionalKey("node", req.Key); err != nil {
		return err
	}

	parsed, err := url.Parse(req.APIURL)
	if err != nil {
		return fmt.Errorf("%w: node apiUrl is invalid: %w", failure.ErrInvalid, err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: node apiUrl must use http or https", failure.ErrInvalid)
	}

	if parsed.Host == "" {
		return fmt.Errorf("%w: node apiUrl must include a host", failure.ErrInvalid)
	}

	return nil
}

func validateNamespaceRequest(req cluster.CreateNamespaceRequest) error {
	if err := services.ValidateOptionalKey("namespace", req.Key); err != nil {
		return err
	}

	if req.Key != nil && strings.HasPrefix(*req.Key, namespaceIDPrefix+"_") {
		return fmt.Errorf("%w: namespace key cannot use reserved %s_ prefix", failure.ErrInvalid, namespaceIDPrefix)
	}

	if req.Limits.VCPU < 0 || req.Limits.MemoryBytes < 0 || req.Limits.VolumeBytes < 0 {
		return fmt.Errorf("%w: namespace limits cannot be negative", failure.ErrInvalid)
	}

	return nil
}

func copyNode(node cluster.Node) cluster.Node {
	node.Key = services.CopyStringPtr(node.Key)

	return node
}

func copyNamespace(namespace cluster.Namespace) cluster.Namespace {
	namespace.Key = services.CopyStringPtr(namespace.Key)

	return namespace
}
