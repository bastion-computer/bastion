package cluster

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/pkg/sshtunnel"
)

// CreateEnvironment handles source environment creation requests.
//
//nolint:dupl,wsl_v5 // Mirrors template stream handling while keeping environment-specific types explicit.
func (h Handler) CreateEnvironment(c *gin.Context) {
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
	created, err := h.cluster.CreateEnvironment(createCtx, namespaceSelector(c), req)
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

// ListEnvironments handles source environment list requests.
func (h Handler) ListEnvironments(c *gin.Context) {
	limit, cursor := handlers.ListParams(c)
	environments, err := h.cluster.ListEnvironments(c.Request.Context(), namespaceSelector(c), limit, cursor, c.QueryArray("tag"))
	handlers.Respond(c, environments, err, http.StatusOK)
}

// GetEnvironmentByID handles source environment lookup by ID requests.
func (h Handler) GetEnvironmentByID(c *gin.Context) {
	environment, err := h.cluster.GetEnvironment(c.Request.Context(), namespaceSelector(c), c.Param("id"), "")
	handlers.Respond(c, environment, err, http.StatusOK)
}

// GetEnvironmentByKey handles source environment lookup by key requests.
func (h Handler) GetEnvironmentByKey(c *gin.Context) {
	environment, err := h.cluster.GetEnvironment(c.Request.Context(), namespaceSelector(c), "", c.Param("key"))
	handlers.Respond(c, environment, err, http.StatusOK)
}

// RemoveEnvironmentByID handles source environment removal by ID requests.
func (h Handler) RemoveEnvironmentByID(c *gin.Context) {
	environment, err := h.cluster.RemoveEnvironment(c.Request.Context(), namespaceSelector(c), c.Param("id"), "")
	handlers.Respond(c, environment, err, http.StatusOK)
}

// RemoveEnvironmentByKey handles source environment removal by key requests.
func (h Handler) RemoveEnvironmentByKey(c *gin.Context) {
	environment, err := h.cluster.RemoveEnvironment(c.Request.Context(), namespaceSelector(c), "", c.Param("key"))
	handlers.Respond(c, environment, err, http.StatusOK)
}

// EnvironmentTunnels handles source environment tunnel metadata lookup requests.
func (h Handler) EnvironmentTunnels(c *gin.Context) {
	var (
		tunnels environment.Tunnels
		err     error
	)

	if key := c.Param("key"); key != "" {
		tunnels, err = h.cluster.GetEnvironmentTunnels(c.Request.Context(), namespaceSelector(c), "", key)
	} else {
		tunnels, err = h.cluster.GetEnvironmentTunnels(c.Request.Context(), namespaceSelector(c), c.Param("id"), "")
	}

	handlers.Respond(c, tunnels, err, http.StatusOK)
}

// EnvironmentSSH proxies an upgraded SSH stream to a derivative environment.
func (h Handler) EnvironmentSSH(c *gin.Context) {
	var req sshtunnel.Request
	if !handlers.BindJSON(c, &req) {
		return
	}

	nodeStream, err := h.cluster.OpenEnvironmentSSH(c.Request.Context(), namespaceSelector(c), c.Param("id"), "", req)
	if err != nil {
		_ = c.Error(err)
		c.JSON(handlers.ErrorStatus(err), gin.H{clusterErrorKey: err.Error()})

		return
	}
	defer func() { _ = nodeStream.Close() }()

	clientStream, err := hijackClusterSSH(c.Writer)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{clusterErrorKey: err.Error()})

		return
	}
	defer func() { _ = clientStream.Close() }()

	proxyRawStreams(c.Request.Context(), clientStream, nodeStream)
}

// EnvironmentAgentProxy proxies HTTP requests to a derivative environment agent.
func (h Handler) EnvironmentAgentProxy(c *gin.Context) {
	h.proxyEnvironmentRoute(c, "/agents/"+url.PathEscape(c.Param("agent")))
}

// EnvironmentTunnelProxy proxies HTTP requests to a derivative environment tunnel.
func (h Handler) EnvironmentTunnelProxy(c *gin.Context) {
	h.proxyEnvironmentRoute(c, "/tunnels/"+url.PathEscape(c.Param("name")))
}

func (h Handler) proxyEnvironmentRoute(c *gin.Context, suffix string) {
	var (
		route clusterserviceRoute
		err   error
	)

	if key := c.Param("key"); key != "" {
		route, err = h.environmentRoute(c, "", key)
	} else {
		route, err = h.environmentRoute(c, c.Param("id"), "")
	}

	if err != nil {
		_ = c.Error(err)
		c.JSON(handlers.ErrorStatus(err), gin.H{clusterErrorKey: err.Error()})

		return
	}

	path := "/v1/environments/" + url.PathEscape(route.derivativeEnvironmentID) + suffix + c.Param("path")
	proxy := newNodeReverseProxy(c, route.nodeURL, path)
	proxy.ServeHTTP(c.Writer, c.Request)
}

type clusterserviceRoute struct {
	nodeURL                 string
	derivativeEnvironmentID string
}

func (h Handler) environmentRoute(c *gin.Context, id, key string) (clusterserviceRoute, error) {
	route, err := h.cluster.GetEnvironmentRoute(c.Request.Context(), namespaceSelector(c), id, key)
	if err != nil {
		return clusterserviceRoute{}, err
	}

	return clusterserviceRoute{nodeURL: route.NodeURL, derivativeEnvironmentID: route.DerivativeEnvironmentID}, nil
}

func newNodeReverseProxy(c *gin.Context, nodeURL, path string) *httputil.ReverseProxy {
	target, err := url.Parse(strings.TrimRight(nodeURL, "/"))
	if err != nil {
		return &httputil.ReverseProxy{ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, err.Error(), http.StatusBadGateway)
		}}
	}

	return &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = joinClusterProxyPath(target.Path, path)
			req.URL.RawPath = ""
			req.URL.RawQuery = joinClusterProxyQuery(target.RawQuery, stripNamespaceQuery(req.URL.Query()))
			req.Host = target.Host
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			_ = c.Error(err)
			http.Error(w, err.Error(), http.StatusBadGateway)
		},
	}
}

func stripNamespaceQuery(query url.Values) string {
	copied := make(url.Values, len(query))
	for key, values := range query {
		if key == "namespace-id" || key == "namespace-key" {
			continue
		}

		copied[key] = append([]string(nil), values...)
	}

	return copied.Encode()
}

func joinClusterProxyQuery(base, request string) string {
	switch {
	case base == "":
		return request
	case request == "":
		return base
	default:
		return base + "&" + request
	}
}

func joinClusterProxyPath(base, path string) string {
	if base == "" || base == "/" {
		return path
	}

	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(path, "/")
}

func hijackClusterSSH(w http.ResponseWriter) (io.ReadWriteCloser, error) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("cluster API server does not support connection hijacking")
	}

	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}

	if _, err := rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: " + sshtunnel.Protocol + "\r\n\r\n"); err != nil {
		_ = conn.Close()

		return nil, err
	}

	if err := rw.Flush(); err != nil {
		_ = conn.Close()

		return nil, err
	}

	return conn, nil
}

func proxyRawStreams(ctx context.Context, left, right io.ReadWriteCloser) {
	done := make(chan struct{}, 2)
	copyStream := func(dst io.WriteCloser, src io.Reader) {
		_, _ = io.Copy(dst, src)
		_ = dst.Close()

		done <- struct{}{}
	}

	go copyStream(left, right)
	go copyStream(right, left)

	select {
	case <-ctx.Done():
		return
	case <-done:
		return
	}
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
