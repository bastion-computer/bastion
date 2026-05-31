// Package queueproxy serves queue worker endpoints on VM TAP host IPs.
package queueproxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bastion-computer/bastion/core/internal/database"
	"github.com/bastion-computer/bastion/core/internal/handlers/queues"
	"github.com/bastion-computer/bastion/core/internal/services/queue"
)

// Manager owns per-host-IP queue proxy listeners.
type Manager struct {
	queues  *queue.Service
	port    int
	logger  *slog.Logger
	mu      sync.Mutex
	servers map[string]*http.Server
}

// NewManager returns a queue proxy manager.
func NewManager(ctx context.Context, queues *queue.Service, port int, logger *slog.Logger) *Manager {
	if port == 0 {
		port = 3150
	}

	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	manager := &Manager{queues: queues, port: port, logger: logger, servers: make(map[string]*http.Server)}

	if ctx != nil {
		go func() {
			<-ctx.Done()

			_ = manager.Close()
		}()
	}

	return manager
}

// Start begins serving queue worker routes on hostIP:port.
func (m *Manager) Start(ctx context.Context, hostIP string) error {
	if hostIP == "" {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.servers[hostIP]; ok {
		return nil
	}

	addr := net.JoinHostPort(hostIP, strconv.Itoa(m.port))

	if ctx == nil {
		ctx = context.Background()
	}

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on queue proxy %s: %w", addr, err)
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           NewRouter(m.queues),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	m.servers[hostIP] = server

	go m.serve(hostIP, listener, server)

	m.logger.Info("queue proxy listening", slog.String("addr", addr))

	return nil
}

// Stop shuts down the listener for hostIP if one is running.
func (m *Manager) Stop(hostIP string) error {
	if hostIP == "" {
		return nil
	}

	m.mu.Lock()

	server, ok := m.servers[hostIP]
	if ok {
		delete(m.servers, hostIP)
	}

	m.mu.Unlock()

	if !ok {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown queue proxy %s: %w", hostIP, err)
	}

	m.logger.Info("queue proxy stopped", slog.String("host_ip", hostIP))

	return nil
}

// StartExisting starts proxies for persisted VM host IPs. Unavailable IPs are logged and skipped.
func (m *Manager) StartExisting(ctx context.Context, db *database.Client) error {
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT host_ip FROM environment_vms WHERE host_ip != ''`)
	if err != nil {
		return fmt.Errorf("list queue proxy host ips: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var hostIP string
		if err := rows.Scan(&hostIP); err != nil {
			return fmt.Errorf("scan queue proxy host ip: %w", err)
		}

		if err := m.Start(ctx, hostIP); err != nil {
			m.logger.WarnContext(ctx, "queue proxy start skipped", slog.String("host_ip", hostIP), slog.String("error", err.Error()))
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate queue proxy host ips: %w", err)
	}

	return nil
}

// Close shuts down all queue proxy listeners.
func (m *Manager) Close() error {
	m.mu.Lock()

	hostIPs := make([]string, 0, len(m.servers))
	for hostIP := range m.servers {
		hostIPs = append(hostIPs, hostIP)
	}

	m.mu.Unlock()

	var errs []error

	for _, hostIP := range hostIPs {
		if err := m.Stop(hostIP); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (m *Manager) serve(hostIP string, listener net.Listener, server *http.Server) {
	err := server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}

	if err != nil {
		m.logger.Error("queue proxy failed", slog.String("host_ip", hostIP), slog.String("error", err.Error()))
	}

	m.mu.Lock()
	if m.servers[hostIP] == server {
		delete(m.servers, hostIP)
	}

	m.mu.Unlock()
}

// NewRouter builds the queue proxy router.
func NewRouter(service *queue.Service) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())

	handler := queues.NewHandler(service)
	v1 := router.Group("/v1")
	queueRoutes := v1.Group("/queues")
	queueRoutes.POST("/by-key/:key/lease", handler.LeaseByKey)
	queueRoutes.POST("/by-key/:key/tasks/:taskID/ack", handler.AckByKey)
	queueRoutes.POST("/by-key/:key/tasks/:taskID/fail", handler.FailByKey)
	queueRoutes.POST("/:id/lease", handler.LeaseByID)
	queueRoutes.POST("/:id/tasks/:taskID/ack", handler.AckByID)
	queueRoutes.POST("/:id/tasks/:taskID/fail", handler.FailByID)

	return router
}
