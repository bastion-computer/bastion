// Package api exposes the Linear integration HTTP API.
//
//nolint:wsl_v5 // HTTP handlers keep validation branches close to their responses.
package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/bastion-computer/bastion/integrations/linear/internal/linear"
	"github.com/bastion-computer/bastion/integrations/linear/internal/service"
)

// Server serves Linear webhooks.
type Server struct {
	addr          string
	webhookSecret string
	service       *service.Service
	logger        *slog.Logger
}

// NewServer returns a Linear integration server.
func NewServer(addr, webhookSecret string, svc *service.Service, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}

	return &Server{addr: addr, webhookSecret: webhookSecret, service: svc, logger: logger}
}

// Run serves requests until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("POST /webhooks/linear", s.linearWebhook)

	server := &http.Server{Addr: s.addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
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
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		return <-errCh
	}
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) linearWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		http.Error(w, "read webhook", http.StatusBadRequest)
		return
	}

	payload, err := linear.ParseVerifiedWebhook(body, r.Header.Get(linear.SignatureHeader()), s.webhookSecret, time.Now())
	if err != nil {
		s.logger.WarnContext(r.Context(), "rejected Linear webhook", slog.String("error", err.Error()))
		http.Error(w, "invalid webhook", http.StatusUnauthorized)
		return
	}

	if err := s.service.AcceptWebhook(r.Context(), payload, body); err != nil {
		s.logger.ErrorContext(r.Context(), "accepted Linear webhook failed", slog.String("error", err.Error()), slog.String("webhook_id", payload.WebhookID))
		http.Error(w, "process webhook", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
