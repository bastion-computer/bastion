// Package main runs the mock Linear API used by integration E2E tests.
//
//nolint:wsl_v5 // Test helper setup is intentionally compact.
package main

import (
	"encoding/json"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/bastion-computer/bastion/integrations/linear/internal/mocklinear"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:3151", "listen address")
	secret := flag.String("webhook-secret", "linear-e2e-secret", "webhook signing secret")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	mock := mocklinear.New(*secret)
	mux := http.NewServeMux()
	mux.Handle("/graphql", mock)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/activities", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mock.Snapshot())
	})

	server := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}

	logger.Info("starting mock Linear API", slog.String("addr", *addr))
	if err := server.ListenAndServe(); err != nil {
		logger.Error("mock Linear API stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}
