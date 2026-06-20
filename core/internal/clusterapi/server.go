// Package clusterapi exposes the Bastion cluster control plane API.
//
//nolint:dupl,goconst,wsl_v5 // Cluster handlers have repeated CRUD and streaming route plumbing.
package clusterapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/client"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/handlers"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/cluster"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/internal/services/secret"
	"github.com/bastion-computer/bastion/core/internal/services/template"
	"github.com/bastion-computer/bastion/core/internal/services/utilization"
)

var templateSecretExpression = regexp.MustCompile(`\$\{\{\s*([A-Za-z][A-Za-z0-9_]*)\.([^}\s]+)\s*\}\}`)

func init() {
	gin.SetMode(gin.ReleaseMode)
}

// NewServer returns an HTTP server configured for the Bastion cluster API.
func NewServer(addr string, store Store, logger *slog.Logger, opts ...RouterOption) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           NewRouter(store, logger, opts...),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

type routerConfig struct {
	archives  ArchiveStore
	newClient func(string) *client.Client
}

// RouterOption configures the cluster router.
type RouterOption func(*routerConfig)

// WithArchiveStore configures template archive storage.
func WithArchiveStore(archives ArchiveStore) RouterOption {
	return func(cfg *routerConfig) {
		cfg.archives = archives
	}
}

// WithHostClientFactory configures host API client construction.
func WithHostClientFactory(factory func(string) *client.Client) RouterOption {
	return func(cfg *routerConfig) {
		cfg.newClient = factory
	}
}

// NewRouter builds the Bastion cluster API router.
func NewRouter(store Store, logger *slog.Logger, opts ...RouterOption) *gin.Engine {
	if store == nil {
		store = NewMemoryStore()
	}
	cfg := routerConfig{archives: NewMemoryArchiveStore(), newClient: func(baseURL string) *client.Client { return client.New(baseURL) }}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.archives == nil {
		cfg.archives = NewMemoryArchiveStore()
	}
	if cfg.newClient == nil {
		cfg.newClient = func(baseURL string) *client.Client { return client.New(baseURL) }
	}

	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	router := gin.New()
	router.Use(recoveryMiddleware(logger))

	v1 := router.Group("/v1")
	clusterRoutes := v1.Group("/cluster")
	clusterRoutes.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	h := handler{store: store, archives: cfg.archives, newClient: cfg.newClient}
	clusterRoutes.GET("/utilization", h.clusterUtilization)

	nodeRoutes := clusterRoutes.Group("/nodes")
	nodeRoutes.POST("", h.createNode)
	nodeRoutes.GET("", h.listNodes)
	nodeRoutes.GET("/by-key/:key", h.getNodeByKey)
	nodeRoutes.DELETE("/by-key/:key", h.removeNodeByKey)
	nodeRoutes.GET("/:id", h.getNodeByID)
	nodeRoutes.DELETE("/:id", h.removeNodeByID)

	namespaceRoutes := clusterRoutes.Group("/namespaces")
	namespaceRoutes.POST("", h.createNamespace)
	namespaceRoutes.GET("", h.listNamespaces)
	namespaceRoutes.GET("/by-key/:key", h.getNamespaceByKey)
	namespaceRoutes.DELETE("/by-key/:key", h.removeNamespaceByKey)
	namespaceRoutes.GET("/:id", h.getNamespaceByID)
	namespaceRoutes.DELETE("/:id", h.removeNamespaceByID)

	registerNamespaceRoutes(v1, h)
	registerNamespaceRequiredRoutes(v1)

	return router
}

// Run starts the Bastion cluster API server and shuts it down when ctx is cancelled.
func Run(ctx context.Context, addr string, store Store, logger *slog.Logger, opts ...RouterOption) error {
	server := NewServer(addr, store, logger, opts...)
	errCh := make(chan error, 1)

	go func() {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}

		errCh <- err
	}()

	select {
	case err := <-errCh:
		if err != nil {
			logger.ErrorContext(ctx, "cluster API failed", slog.String("error", err.Error()))
		}

		return err
	case <-ctx.Done():
		logger.InfoContext(context.Background(), "cluster API shutting down", slog.String("addr", addr))

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown cluster API: %w", err)
		}

		if err := <-errCh; err != nil {
			return err
		}

		logger.InfoContext(context.Background(), "cluster API stopped", slog.String("addr", addr))

		return nil
	}
}

type handler struct {
	store     Store
	archives  ArchiveStore
	newClient func(string) *client.Client
}

func registerNamespaceRoutes(v1 *gin.RouterGroup, h handler) {
	ns := v1.Group("/namespaces/:namespace")
	ns.GET("/health", h.namespaceHealth)
	ns.GET("/utilization", h.namespaceUtilization)

	secretRoutes := ns.Group("/secrets")
	secretRoutes.POST("", h.createSecret)
	secretRoutes.GET("", h.listSecrets)
	secretRoutes.GET("/by-key/:key", h.getSecretByKey)
	secretRoutes.DELETE("/by-key/:key", h.removeSecretByKey)
	secretRoutes.GET("/:id", h.getSecretByID)
	secretRoutes.DELETE("/:id", h.removeSecretByID)

	templateRoutes := ns.Group("/templates")
	templateRoutes.POST("", h.createTemplate)
	templateRoutes.GET("", h.listTemplates)
	templateRoutes.GET("/by-key/:key", h.getTemplateByKey)
	templateRoutes.GET("/by-key/:key/export", h.exportTemplateByKey)
	templateRoutes.DELETE("/by-key/:key", h.removeTemplateByKey)
	templateRoutes.GET("/:id", h.getTemplateByID)
	templateRoutes.GET("/:id/export", h.exportTemplateByID)
	templateRoutes.DELETE("/:id", h.removeTemplateByID)

	environmentRoutes := ns.Group("/environments")
	environmentRoutes.POST("", h.createEnvironment)
	environmentRoutes.GET("", h.listEnvironments)
	environmentRoutes.GET("/by-key/:key", h.getEnvironmentByKey)
	environmentRoutes.GET("/by-key/:key/tunnels", h.environmentTunnelsByKey)
	environmentRoutes.DELETE("/by-key/:key", h.removeEnvironmentByKey)
	environmentRoutes.Any("/by-key/:key/agents/:agent", h.environmentAgentProxyByKey)
	environmentRoutes.Any("/by-key/:key/agents/:agent/*path", h.environmentAgentProxyByKey)
	environmentRoutes.Any("/by-key/:key/tunnels/:name", h.environmentTunnelProxyByKey)
	environmentRoutes.Any("/by-key/:key/tunnels/:name/*path", h.environmentTunnelProxyByKey)
	environmentRoutes.POST("/:id/ssh", h.environmentSSHProxy)
	environmentRoutes.GET("/:id/tunnels", h.environmentTunnelsByID)
	environmentRoutes.Any("/:id/agents/:agent", h.environmentAgentProxyByID)
	environmentRoutes.Any("/:id/agents/:agent/*path", h.environmentAgentProxyByID)
	environmentRoutes.Any("/:id/tunnels/:name", h.environmentTunnelProxyByID)
	environmentRoutes.Any("/:id/tunnels/:name/*path", h.environmentTunnelProxyByID)
	environmentRoutes.GET("/:id", h.getEnvironmentByID)
	environmentRoutes.DELETE("/:id", h.removeEnvironmentByID)
}

func registerNamespaceRequiredRoutes(v1 *gin.RouterGroup) {
	respond := func(c *gin.Context) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "namespace is required"})
	}

	v1.GET("/health", respond)
	v1.GET("/utilization", respond)
	for _, path := range []string{"/secrets", "/secrets/*path", "/templates", "/templates/*path", "/environments", "/environments/*path"} {
		v1.Any(path, respond)
	}
}

func (h handler) createNode(c *gin.Context) {
	var req cluster.CreateNodeRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	created, err := h.store.CreateNode(c.Request.Context(), req)
	handlers.Respond(c, created, err, http.StatusCreated)
}

func (h handler) listNodes(c *gin.Context) {
	limit, cursor := handlers.ListParams(c)
	nodes, err := h.store.ListNodes(c.Request.Context(), limit, cursor)
	handlers.Respond(c, nodes, err, http.StatusOK)
}

func (h handler) getNodeByID(c *gin.Context) {
	node, err := h.store.GetNode(c.Request.Context(), c.Param("id"), "")
	handlers.Respond(c, node, err, http.StatusOK)
}

func (h handler) getNodeByKey(c *gin.Context) {
	node, err := h.store.GetNode(c.Request.Context(), "", c.Param("key"))
	handlers.Respond(c, node, err, http.StatusOK)
}

func (h handler) removeNodeByID(c *gin.Context) {
	node, err := h.store.RemoveNode(c.Request.Context(), c.Param("id"), "")
	handlers.Respond(c, node, err, http.StatusOK)
}

func (h handler) removeNodeByKey(c *gin.Context) {
	node, err := h.store.RemoveNode(c.Request.Context(), "", c.Param("key"))
	handlers.Respond(c, node, err, http.StatusOK)
}

func (h handler) createNamespace(c *gin.Context) {
	var req cluster.CreateNamespaceRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	created, err := h.store.CreateNamespace(c.Request.Context(), req)
	handlers.Respond(c, created, err, http.StatusCreated)
}

func (h handler) listNamespaces(c *gin.Context) {
	limit, cursor := handlers.ListParams(c)
	namespaces, err := h.store.ListNamespaces(c.Request.Context(), limit, cursor)
	handlers.Respond(c, namespaces, err, http.StatusOK)
}

func (h handler) getNamespaceByID(c *gin.Context) {
	namespace, err := h.store.GetNamespace(c.Request.Context(), c.Param("id"), "")
	handlers.Respond(c, namespace, err, http.StatusOK)
}

func (h handler) getNamespaceByKey(c *gin.Context) {
	namespace, err := h.store.GetNamespace(c.Request.Context(), "", c.Param("key"))
	handlers.Respond(c, namespace, err, http.StatusOK)
}

func (h handler) removeNamespaceByID(c *gin.Context) {
	namespace, err := h.store.RemoveNamespace(c.Request.Context(), c.Param("id"), "")
	handlers.Respond(c, namespace, err, http.StatusOK)
}

func (h handler) removeNamespaceByKey(c *gin.Context) {
	namespace, err := h.store.RemoveNamespace(c.Request.Context(), "", c.Param("key"))
	handlers.Respond(c, namespace, err, http.StatusOK)
}

func (h handler) clusterUtilization(c *gin.Context) {
	utilization, err := h.aggregateClusterUtilization(c.Request.Context())
	handlers.Respond(c, utilization, err, http.StatusOK)
}

func (h handler) aggregateClusterUtilization(ctx context.Context) (utilization.Utilization, error) {
	var out utilization.Utilization
	cursor := ""

	for {
		nodes, err := h.store.ListNodes(ctx, 100, cursor)
		if err != nil {
			return utilization.Utilization{}, err
		}

		for _, node := range nodes.Entries {
			nodeUtilization, err := h.newClient(node.APIURL).GetUtilization(ctx)
			if err != nil {
				return utilization.Utilization{}, fmt.Errorf("%w: get utilization for node %s: %w", failure.ErrFailedDependency, node.ID, err)
			}

			out.VCPU = addResource(out.VCPU, nodeUtilization.VCPU)
			out.Memory = addResource(out.Memory, nodeUtilization.Memory)
			out.Volume = addResource(out.Volume, nodeUtilization.Volume)
		}

		if nodes.Cursor == nil {
			return out, nil
		}

		cursor = *nodes.Cursor
	}
}

func addResource(a, b utilization.Resource) utilization.Resource {
	return utilization.Resource{Total: a.Total + b.Total, Used: a.Used + b.Used, Available: a.Available + b.Available}
}

func (h handler) namespaceHealth(c *gin.Context) {
	_, err := h.namespace(c)
	handlers.Respond(c, gin.H{"status": "ok"}, err, http.StatusOK)
}

func (h handler) namespaceUtilization(c *gin.Context) {
	_, err := h.namespace(c)
	handlers.Respond(c, utilization.Utilization{}, err, http.StatusOK)
}

func (h handler) createSecret(c *gin.Context) {
	namespace, ok := h.resolveNamespace(c)
	if !ok {
		return
	}

	var req secret.CreateRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	created, err := h.store.CreateSecret(c.Request.Context(), namespace.ID, req)
	handlers.Respond(c, created, err, http.StatusCreated)
}

func (h handler) listSecrets(c *gin.Context) {
	namespace, ok := h.resolveNamespace(c)
	if !ok {
		return
	}

	limit, cursor := handlers.ListParams(c)
	secrets, err := h.store.ListSecrets(c.Request.Context(), namespace.ID, limit, cursor)
	handlers.Respond(c, secrets, err, http.StatusOK)
}

func (h handler) getSecretByID(c *gin.Context) {
	h.getSecret(c, c.Param("id"), "")
}

func (h handler) getSecretByKey(c *gin.Context) {
	h.getSecret(c, "", c.Param("key"))
}

func (h handler) getSecret(c *gin.Context, id, key string) {
	namespace, ok := h.resolveNamespace(c)
	if !ok {
		return
	}

	secret, err := h.store.GetSecret(c.Request.Context(), namespace.ID, id, key)
	handlers.Respond(c, secret, err, http.StatusOK)
}

func (h handler) removeSecretByID(c *gin.Context) {
	h.removeSecret(c, c.Param("id"), "")
}

func (h handler) removeSecretByKey(c *gin.Context) {
	h.removeSecret(c, "", c.Param("key"))
}

func (h handler) removeSecret(c *gin.Context, id, key string) {
	namespace, ok := h.resolveNamespace(c)
	if !ok {
		return
	}

	secret, err := h.store.RemoveSecret(c.Request.Context(), namespace.ID, id, key)
	handlers.Respond(c, secret, err, http.StatusOK)
}

func (h handler) createTemplate(c *gin.Context) {
	var req template.CreateRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	createCtx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	stream := newTemplateCreateStream(c.Writer, cancel)
	if err := stream.Start(); err != nil {
		_ = c.Error(err)

		return
	}

	req.Logs = stream

	created, err := h.createTemplateRecord(createCtx, c.Param("namespace"), req)
	if err != nil {
		_ = c.Error(err)
		if writeErr := stream.Error(err); writeErr != nil {
			_ = c.Error(writeErr)
		}

		return
	}

	if err := stream.Result(created); err != nil {
		_ = c.Error(err)
	}
}

func (h handler) createTemplateRecord(ctx context.Context, namespaceRef string, req template.CreateRequest) (template.Metadata, error) {
	namespace, err := h.store.ResolveNamespace(ctx, namespaceRef)
	if err != nil {
		return template.Metadata{}, err
	}

	if len(req.Config) == 0 || !json.Valid(req.Config) {
		return template.Metadata{}, fmt.Errorf("%w: template config must be valid JSON", failure.ErrInvalid)
	}

	node, err := h.selectNode(ctx)
	if err != nil {
		return template.Metadata{}, err
	}

	api := h.newClient(node.APIURL)
	derivativeConfig, derivativeSecrets, err := h.prepareDerivativeTemplateConfig(ctx, api, namespace.ID, req.Config)
	if err != nil {
		return template.Metadata{}, err
	}
	defer cleanupDerivativeSecrets(api, derivativeSecrets)

	derivative, err := api.CreateTemplate(ctx, template.CreateRequest{Config: derivativeConfig, Logs: req.Logs})
	if err != nil {
		return template.Metadata{}, err
	}

	cleanupDerivative := true
	defer func() {
		if cleanupDerivative {
			_, _ = api.RemoveTemplate(context.Background(), derivative.ID, "")
		}
	}()

	var archive bytes.Buffer
	if err := api.ExportTemplate(ctx, derivative.ID, "", &archive); err != nil {
		return template.Metadata{}, err
	}

	sourceID, err := services.GenerateID("tpl")
	if err != nil {
		return template.Metadata{}, err
	}

	archiveKey := "templates/" + sourceID + ".tar.zst"
	if err := h.archives.Put(ctx, archiveKey, archive.Bytes()); err != nil {
		return template.Metadata{}, err
	}

	created, err := h.store.CreateTemplate(ctx, namespace.ID, template.Template{ID: sourceID, Key: services.CopyStringPtr(req.Key), Config: append(json.RawMessage(nil), req.Config...), CreatedAt: services.Now()}, archiveKey)
	if err != nil {
		_ = h.archives.Delete(context.Background(), archiveKey)

		return template.Metadata{}, err
	}

	if _, err := api.RemoveTemplate(ctx, derivative.ID, ""); err != nil {
		return template.Metadata{}, err
	}
	cleanupDerivative = false

	return created, nil
}

func (h handler) selectNode(ctx context.Context) (cluster.Node, error) {
	nodes, err := h.store.ListNodes(ctx, 1, "")
	if err != nil {
		return cluster.Node{}, err
	}

	if len(nodes.Entries) == 0 {
		return cluster.Node{}, fmt.Errorf("%w: no cluster nodes are available", failure.ErrFailedDependency)
	}

	return nodes.Entries[0], nil
}

func (h handler) prepareDerivativeTemplateConfig(ctx context.Context, api *client.Client, namespaceID string, config json.RawMessage) (json.RawMessage, []string, error) {
	refs, err := collectTemplateSecretReferences(config)
	if err != nil {
		return nil, nil, err
	}

	mapping := make(map[string]string, len(refs))
	derivativeSecrets := make([]string, 0, len(refs))
	for _, ref := range refs {
		id, key := ref, ""
		if !strings.HasPrefix(ref, "sec_") {
			id, key = "", ref
		}

		source, err := h.store.GetSecret(ctx, namespaceID, id, key)
		if errors.Is(err, failure.ErrNotFound) {
			return nil, derivativeSecrets, fmt.Errorf("%w: secret %s not found", failure.ErrInvalid, ref)
		}

		if err != nil {
			return nil, derivativeSecrets, err
		}

		derivative, err := api.CreateSecret(ctx, secret.CreateRequest{Value: source.Value})
		if err != nil {
			return nil, derivativeSecrets, err
		}

		mapping[ref] = derivative.ID
		derivativeSecrets = append(derivativeSecrets, derivative.ID)
	}

	prepared, err := rewriteTemplateSecretReferences(config, mapping)
	if err != nil {
		return nil, derivativeSecrets, err
	}

	return prepared, derivativeSecrets, nil
}

func cleanupDerivativeSecrets(api *client.Client, secretIDs []string) {
	for _, secretID := range secretIDs {
		_, _ = api.RemoveSecret(context.Background(), secretID, "")
	}
}

func collectTemplateSecretReferences(config json.RawMessage) ([]string, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(config))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: parse template config: %w", failure.ErrInvalid, err)
	}

	seen := map[string]struct{}{}
	if err := collectTemplateSecretReferencesValue(value, seen); err != nil {
		return nil, err
	}

	refs := make([]string, 0, len(seen))
	for ref := range seen {
		refs = append(refs, ref)
	}

	slices.Sort(refs)

	return refs, nil
}

func collectTemplateSecretReferencesValue(value any, seen map[string]struct{}) error {
	switch value := value.(type) {
	case map[string]any:
		for _, child := range value {
			if err := collectTemplateSecretReferencesValue(child, seen); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range value {
			if err := collectTemplateSecretReferencesValue(child, seen); err != nil {
				return err
			}
		}
	case string:
		matches := templateSecretExpression.FindAllStringSubmatch(value, -1)
		for _, match := range matches {
			if match[1] != "secret" {
				return fmt.Errorf("%w: unsupported template expression %s.%s", failure.ErrInvalid, match[1], match[2])
			}

			seen[match[2]] = struct{}{}
		}
	}

	return nil
}

func rewriteTemplateSecretReferences(config json.RawMessage, mapping map[string]string) (json.RawMessage, error) {
	if len(mapping) == 0 {
		return append(json.RawMessage(nil), config...), nil
	}

	var value any
	decoder := json.NewDecoder(bytes.NewReader(config))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: parse template config: %w", failure.ErrInvalid, err)
	}

	rewritten, err := rewriteTemplateSecretReferenceValue(value, mapping)
	if err != nil {
		return nil, err
	}

	contents, err := json.Marshal(rewritten)
	if err != nil {
		return nil, fmt.Errorf("rewrite template config: %w", err)
	}

	return json.RawMessage(contents), nil
}

func rewriteTemplateSecretReferenceValue(value any, mapping map[string]string) (any, error) {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			rewritten, err := rewriteTemplateSecretReferenceValue(child, mapping)
			if err != nil {
				return nil, err
			}

			value[key] = rewritten
		}

		return value, nil
	case []any:
		for index, child := range value {
			rewritten, err := rewriteTemplateSecretReferenceValue(child, mapping)
			if err != nil {
				return nil, err
			}

			value[index] = rewritten
		}

		return value, nil
	case string:
		return rewriteTemplateSecretReferenceString(value, mapping)
	default:
		return value, nil
	}
}

func rewriteTemplateSecretReferenceString(value string, mapping map[string]string) (string, error) {
	var err error
	rewritten := templateSecretExpression.ReplaceAllStringFunc(value, func(match string) string {
		parts := templateSecretExpression.FindStringSubmatch(match)
		if len(parts) != 3 || parts[1] != "secret" {
			err = fmt.Errorf("%w: unsupported template expression", failure.ErrInvalid)

			return match
		}

		derivativeID, ok := mapping[parts[2]]
		if !ok {
			return match
		}

		return "${{ secret." + derivativeID + " }}"
	})

	return rewritten, err
}

func (h handler) listTemplates(c *gin.Context) {
	namespace, ok := h.resolveNamespace(c)
	if !ok {
		return
	}

	limit, cursor := handlers.ListParams(c)
	templates, err := h.store.ListTemplates(c.Request.Context(), namespace.ID, limit, cursor)
	handlers.Respond(c, templates, err, http.StatusOK)
}

func (h handler) getTemplateByID(c *gin.Context) {
	h.getTemplate(c, c.Param("id"), "")
}

func (h handler) getTemplateByKey(c *gin.Context) {
	h.getTemplate(c, "", c.Param("key"))
}

func (h handler) getTemplate(c *gin.Context, id, key string) {
	namespace, ok := h.resolveNamespace(c)
	if !ok {
		return
	}

	stored, err := h.store.GetTemplate(c.Request.Context(), namespace.ID, id, key)
	handlers.Respond(c, stored.Template, err, http.StatusOK)
}

func (h handler) exportTemplateByID(c *gin.Context) {
	h.exportTemplate(c, c.Param("id"), "")
}

func (h handler) exportTemplateByKey(c *gin.Context) {
	h.exportTemplate(c, "", c.Param("key"))
}

func (h handler) exportTemplate(c *gin.Context, id, key string) {
	namespace, ok := h.resolveNamespace(c)
	if !ok {
		return
	}

	stored, err := h.store.GetTemplate(c.Request.Context(), namespace.ID, id, key)
	if err != nil {
		handlers.Respond(c, nil, err, http.StatusOK)

		return
	}

	archive, err := h.archives.Get(c.Request.Context(), stored.ArchiveKey)
	if err != nil {
		handlers.Respond(c, nil, err, http.StatusOK)

		return
	}

	c.Header("Content-Type", template.ArchiveContentType)
	_, _ = c.Writer.Write(archive)
}

func (h handler) removeTemplateByID(c *gin.Context) {
	h.removeTemplate(c, c.Param("id"), "")
}

func (h handler) removeTemplateByKey(c *gin.Context) {
	h.removeTemplate(c, "", c.Param("key"))
}

func (h handler) removeTemplate(c *gin.Context, id, key string) {
	namespace, ok := h.resolveNamespace(c)
	if !ok {
		return
	}

	stored, err := h.store.RemoveTemplate(c.Request.Context(), namespace.ID, id, key)
	if err == nil {
		_ = h.archives.Delete(context.Background(), stored.ArchiveKey)
	}

	handlers.Respond(c, stored.Template, err, http.StatusOK)
}

func (h handler) createEnvironment(c *gin.Context) {
	var req environment.CreateRequest
	if !handlers.BindJSON(c, &req) {
		return
	}

	createCtx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	stream := newEnvironmentCreateStream(c.Writer, cancel)
	if err := stream.Start(); err != nil {
		_ = c.Error(err)

		return
	}

	req.Logs = stream

	created, err := h.createEnvironmentRecord(createCtx, c.Param("namespace"), req)
	if err != nil {
		_ = c.Error(err)
		if writeErr := stream.Error(err); writeErr != nil {
			_ = c.Error(writeErr)
		}

		return
	}

	if err := stream.Result(created); err != nil {
		_ = c.Error(err)
	}
}

func (h handler) createEnvironmentRecord(ctx context.Context, namespaceRef string, req environment.CreateRequest) (environment.Environment, error) {
	if err := services.RequireIDOrKey(req.TemplateID, req.TemplateKey); err != nil {
		return environment.Environment{}, err
	}

	if err := services.ValidateOptionalKey("environment", req.Key); err != nil {
		return environment.Environment{}, err
	}

	namespace, err := h.store.ResolveNamespace(ctx, namespaceRef)
	if err != nil {
		return environment.Environment{}, err
	}

	sourceTemplate, err := h.store.GetTemplate(ctx, namespace.ID, req.TemplateID, req.TemplateKey)
	if err != nil {
		return environment.Environment{}, err
	}

	node, derivativeTemplateID, err := h.selectEnvironmentNode(ctx, sourceTemplate)
	if err != nil {
		return environment.Environment{}, err
	}

	api := h.newClient(node.APIURL)
	if derivativeTemplateID == "" {
		archive, err := h.archives.Get(ctx, sourceTemplate.ArchiveKey)
		if err != nil {
			return environment.Environment{}, err
		}

		imported, err := api.ImportTemplate(ctx, template.ImportRequest{Archive: bytes.NewReader(archive), ArchiveSize: int64(len(archive))})
		if err != nil {
			return environment.Environment{}, err
		}

		derivativeTemplateID = imported.ID
		if err := h.store.SaveTemplateDerivative(ctx, sourceTemplate.ID, node.ID, derivativeTemplateID); err != nil {
			_, _ = api.RemoveTemplate(context.Background(), derivativeTemplateID, "")

			return environment.Environment{}, err
		}
	}

	derivative, err := api.CreateEnvironment(ctx, environment.CreateRequest{TemplateID: derivativeTemplateID, Tags: req.Tags, Logs: req.Logs})
	if err != nil {
		return environment.Environment{}, err
	}

	sourceID, err := services.GenerateID("env")
	if err != nil {
		_, _ = api.RemoveEnvironment(context.Background(), derivative.ID)

		return environment.Environment{}, err
	}

	now := services.Now()
	source := environment.Environment{
		ID:         sourceID,
		Key:        services.CopyStringPtr(req.Key),
		Status:     derivative.Status,
		TemplateID: sourceTemplate.ID,
		Tags:       append([]string(nil), req.Tags...),
		CreatedAt:  now,
		UpdatedAt:  now,
		LastError:  derivative.LastError,
	}

	return h.store.CreateEnvironment(ctx, namespace.ID, EnvironmentRecord{
		Environment:             source,
		NodeID:                  node.ID,
		DerivativeTemplateID:    derivativeTemplateID,
		DerivativeEnvironmentID: derivative.ID,
	})
}

func (h handler) selectEnvironmentNode(ctx context.Context, sourceTemplate StoredTemplate) (cluster.Node, string, error) {
	nodes, err := h.store.ListNodes(ctx, 100, "")
	if err != nil {
		return cluster.Node{}, "", err
	}

	if len(nodes.Entries) == 0 {
		return cluster.Node{}, "", fmt.Errorf("%w: no cluster nodes are available", failure.ErrFailedDependency)
	}

	for _, node := range nodes.Entries {
		derivativeTemplateID, ok, err := h.store.TemplateDerivative(ctx, sourceTemplate.ID, node.ID)
		if err != nil {
			return cluster.Node{}, "", err
		}

		if ok {
			return node, derivativeTemplateID, nil
		}
	}

	return nodes.Entries[0], "", nil
}

func (h handler) listEnvironments(c *gin.Context) {
	namespace, ok := h.resolveNamespace(c)
	if !ok {
		return
	}

	limit, cursor := handlers.ListParams(c)
	environments, err := h.store.ListEnvironments(c.Request.Context(), namespace.ID, limit, cursor, c.QueryArray("tag"))
	handlers.Respond(c, environments, err, http.StatusOK)
}

func (h handler) getEnvironmentByID(c *gin.Context) {
	h.getEnvironment(c, c.Param("id"), "")
}

func (h handler) getEnvironmentByKey(c *gin.Context) {
	h.getEnvironment(c, "", c.Param("key"))
}

func (h handler) getEnvironment(c *gin.Context, id, key string) {
	namespace, ok := h.resolveNamespace(c)
	if !ok {
		return
	}

	record, err := h.store.GetEnvironment(c.Request.Context(), namespace.ID, id, key)
	handlers.Respond(c, record.Environment, err, http.StatusOK)
}

func (h handler) removeEnvironmentByID(c *gin.Context) {
	h.removeEnvironment(c, c.Param("id"), "")
}

func (h handler) removeEnvironmentByKey(c *gin.Context) {
	h.removeEnvironment(c, "", c.Param("key"))
}

func (h handler) removeEnvironment(c *gin.Context, id, key string) {
	namespace, ok := h.resolveNamespace(c)
	if !ok {
		return
	}

	record, err := h.store.GetEnvironment(c.Request.Context(), namespace.ID, id, key)
	if err != nil {
		handlers.Respond(c, environment.Environment{}, err, http.StatusOK)

		return
	}

	node, err := h.store.GetNode(c.Request.Context(), record.NodeID, "")
	if err != nil {
		handlers.Respond(c, environment.Environment{}, err, http.StatusOK)

		return
	}

	removedDerivative, err := h.newClient(node.APIURL).RemoveEnvironment(c.Request.Context(), record.DerivativeEnvironmentID)
	if err != nil {
		handlers.Respond(c, environment.Environment{}, err, http.StatusOK)

		return
	}

	removed, err := h.store.RemoveEnvironment(c.Request.Context(), namespace.ID, id, key)
	if err != nil {
		handlers.Respond(c, environment.Environment{}, err, http.StatusOK)

		return
	}

	out := removed.Environment
	out.Status = removedDerivative.Status
	out.UpdatedAt = services.Now()
	handlers.Respond(c, out, nil, http.StatusOK)
}

func (h handler) environmentTunnelsByID(c *gin.Context) {
	h.environmentTunnels(c, c.Param("id"), "")
}

func (h handler) environmentTunnelsByKey(c *gin.Context) {
	h.environmentTunnels(c, "", c.Param("key"))
}

func (h handler) environmentTunnels(c *gin.Context, id, key string) {
	namespace, ok := h.resolveNamespace(c)
	if !ok {
		return
	}

	record, err := h.store.GetEnvironment(c.Request.Context(), namespace.ID, id, key)
	if err != nil {
		handlers.Respond(c, environment.Tunnels{}, err, http.StatusOK)

		return
	}

	node, err := h.store.GetNode(c.Request.Context(), record.NodeID, "")
	if err != nil {
		handlers.Respond(c, environment.Tunnels{}, err, http.StatusOK)

		return
	}

	tunnels, err := h.newClient(node.APIURL).GetEnvironmentTunnels(c.Request.Context(), record.DerivativeEnvironmentID, "")
	handlers.Respond(c, tunnels, err, http.StatusOK)
}

func (h handler) environmentSSHProxy(c *gin.Context) {
	h.proxyEnvironment(c, c.Param("id"), "", "/ssh")
}

func (h handler) environmentAgentProxyByID(c *gin.Context) {
	h.proxyEnvironment(c, c.Param("id"), "", "/agents/"+url.PathEscape(c.Param("agent"))+c.Param("path"))
}

func (h handler) environmentAgentProxyByKey(c *gin.Context) {
	h.proxyEnvironment(c, "", c.Param("key"), "/agents/"+url.PathEscape(c.Param("agent"))+c.Param("path"))
}

func (h handler) environmentTunnelProxyByID(c *gin.Context) {
	h.proxyEnvironment(c, c.Param("id"), "", "/tunnels/"+url.PathEscape(c.Param("name"))+c.Param("path"))
}

func (h handler) environmentTunnelProxyByKey(c *gin.Context) {
	h.proxyEnvironment(c, "", c.Param("key"), "/tunnels/"+url.PathEscape(c.Param("name"))+c.Param("path"))
}

func (h handler) proxyEnvironment(c *gin.Context, id, key, suffix string) {
	namespace, ok := h.resolveNamespace(c)
	if !ok {
		return
	}

	record, node, err := h.environmentPlacement(c.Request.Context(), namespace.ID, id, key)
	if err != nil {
		_ = c.Error(err)
		c.JSON(handlers.ErrorStatus(err), gin.H{"error": err.Error()})

		return
	}

	target, err := url.Parse(node.APIURL)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})

		return
	}

	path := "/v1/environments/" + url.PathEscape(record.DerivativeEnvironmentID) + suffix
	proxy := &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			req.SetURL(target)
			req.Out.URL.Path = path
			req.Out.URL.RawPath = ""
			req.Out.Host = target.Host
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			_ = c.Error(err)
			http.Error(w, err.Error(), http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(reverseProxyResponseWriter{ResponseWriter: c.Writer}, c.Request)
}

func (h handler) environmentPlacement(ctx context.Context, namespaceID, id, key string) (EnvironmentRecord, cluster.Node, error) {
	record, err := h.store.GetEnvironment(ctx, namespaceID, id, key)
	if err != nil {
		return EnvironmentRecord{}, cluster.Node{}, err
	}

	node, err := h.store.GetNode(ctx, record.NodeID, "")
	if err != nil {
		return EnvironmentRecord{}, cluster.Node{}, err
	}

	return record, node, nil
}

type reverseProxyResponseWriter struct {
	http.ResponseWriter
}

func (w reverseProxyResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w reverseProxyResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return http.NewResponseController(w.ResponseWriter).Hijack()
}

func (w reverseProxyResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (h handler) namespace(c *gin.Context) (cluster.Namespace, error) {
	return h.store.ResolveNamespace(c.Request.Context(), c.Param("namespace"))
}

func (h handler) resolveNamespace(c *gin.Context) (cluster.Namespace, bool) {
	namespace, err := h.namespace(c)
	if err != nil {
		_ = c.Error(err)
		c.JSON(handlers.ErrorStatus(err), gin.H{"error": err.Error()})

		return cluster.Namespace{}, false
	}

	return namespace, true
}

type templateCreateStream struct {
	w          http.ResponseWriter
	encoder    *json.Encoder
	controller *http.ResponseController
	cancel     context.CancelFunc
	mu         sync.Mutex
}

func newTemplateCreateStream(w http.ResponseWriter, cancel context.CancelFunc) *templateCreateStream {
	return &templateCreateStream{w: w, encoder: json.NewEncoder(w), controller: http.NewResponseController(w), cancel: cancel}
}

func (s *templateCreateStream) Start() error {
	s.w.Header().Set("Content-Type", "application/x-ndjson")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.WriteHeader(http.StatusOK)

	return s.flush()
}

func (s *templateCreateStream) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if err := s.write(template.CreateStreamEvent{Type: template.StreamEventLog, Log: string(p)}); err != nil {
		return 0, err
	}

	return len(p), nil
}

func (s *templateCreateStream) Result(created template.Metadata) error {
	return s.write(template.CreateStreamEvent{Type: template.StreamEventResult, Template: &created})
}

func (s *templateCreateStream) Error(err error) error {
	return s.write(template.CreateStreamEvent{Type: template.StreamEventError, Error: err.Error(), Status: handlers.ErrorStatus(err)})
}

func (s *templateCreateStream) write(event template.CreateStreamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.encoder.Encode(event); err != nil {
		s.cancel()

		return err
	}

	return s.flush()
}

func (s *templateCreateStream) flush() error {
	if err := s.controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.cancel()

		return err
	}

	return nil
}

type environmentCreateStream struct {
	w          http.ResponseWriter
	encoder    *json.Encoder
	controller *http.ResponseController
	cancel     context.CancelFunc
	mu         sync.Mutex
}

func newEnvironmentCreateStream(w http.ResponseWriter, cancel context.CancelFunc) *environmentCreateStream {
	return &environmentCreateStream{w: w, encoder: json.NewEncoder(w), controller: http.NewResponseController(w), cancel: cancel}
}

func (s *environmentCreateStream) Start() error {
	s.w.Header().Set("Content-Type", "application/x-ndjson")
	s.w.Header().Set("Cache-Control", "no-cache")
	s.w.WriteHeader(http.StatusOK)

	return s.flush()
}

func (s *environmentCreateStream) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	if err := s.write(environment.CreateStreamEvent{Type: environment.StreamEventLog, Log: string(p)}); err != nil {
		return 0, err
	}

	return len(p), nil
}

func (s *environmentCreateStream) Result(created environment.Environment) error {
	return s.write(environment.CreateStreamEvent{Type: environment.StreamEventResult, Environment: &created})
}

func (s *environmentCreateStream) Error(err error) error {
	return s.write(environment.CreateStreamEvent{Type: environment.StreamEventError, Error: err.Error(), Status: handlers.ErrorStatus(err)})
}

func (s *environmentCreateStream) write(event environment.CreateStreamEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.encoder.Encode(event); err != nil {
		s.cancel()

		return err
	}

	return s.flush()
}

func (s *environmentCreateStream) flush() error {
	if err := s.controller.Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.cancel()

		return err
	}

	return nil
}

func recoveryMiddleware(logger *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		logger.ErrorContext(c.Request.Context(), "cluster request panic", slog.Any("panic", recovered))
		c.AbortWithStatus(http.StatusInternalServerError)
	})
}
