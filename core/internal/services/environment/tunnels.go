//go:build !darwin

package environment

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
	"github.com/bastion-computer/bastion/core/internal/failure"
)

// Tunnels returns the template-registered tunnels for an environment.
func (s *Service) Tunnels(ctx context.Context, environmentID string) (Tunnels, error) {
	return s.tunnels(ctx, environmentID, "")
}

// TunnelsByKey returns the template-registered tunnels for an environment key.
func (s *Service) TunnelsByKey(ctx context.Context, key string) (Tunnels, error) {
	return s.tunnels(ctx, "", key)
}

// TunnelConnection returns private HTTP connection metadata for an environment tunnel.
func (s *Service) TunnelConnection(ctx context.Context, environmentID, name string) (TunnelConnection, error) {
	return s.tunnelConnection(ctx, environmentID, "", name)
}

// TunnelConnectionByKey returns private HTTP connection metadata for an environment tunnel by environment key.
func (s *Service) TunnelConnectionByKey(ctx context.Context, key, name string) (TunnelConnection, error) {
	return s.tunnelConnection(ctx, "", key, name)
}

func (s *Service) tunnels(ctx context.Context, environmentID, key string) (Tunnels, error) {
	environment, err := s.getByIDOrKey(ctx, environmentID, key)
	if err != nil {
		return Tunnels{}, err
	}

	if environment.Status != ch.StateRunning {
		return Tunnels{}, fmt.Errorf("%w: environment status is %q, want running", failure.ErrFailedDependency, environment.Status)
	}

	config, err := s.templateConfig(ctx, environment.TemplateID)
	if err != nil {
		return Tunnels{}, err
	}

	entries, err := tunnelsFromConfig(config)
	if err != nil {
		return Tunnels{}, fmt.Errorf("%w: %w", failure.ErrFailedDependency, err)
	}

	return Tunnels{Entries: entries}, nil
}

func (s *Service) tunnelConnection(ctx context.Context, environmentID, key, name string) (TunnelConnection, error) {
	tunnels, err := s.tunnels(ctx, environmentID, key)
	if err != nil {
		return TunnelConnection{}, err
	}

	var port int

	for _, tunnel := range tunnels.Entries {
		if tunnel.Name == name {
			port = tunnel.Port
			break
		}
	}

	if port == 0 {
		return TunnelConnection{}, fmt.Errorf("%w: environment tunnel %s not found", failure.ErrNotFound, name)
	}

	environment, err := s.getByIDOrKey(ctx, environmentID, key)
	if err != nil {
		return TunnelConnection{}, err
	}

	record, err := s.getRecord(ctx, environment.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return TunnelConnection{}, fmt.Errorf("%w: environment not found", failure.ErrNotFound)
	}

	if err != nil {
		return TunnelConnection{}, fmt.Errorf("get environment tunnel metadata: %w", err)
	}

	connection := TunnelConnection{VsockSocketPath: record.VsockSocketPath}
	if connection.VsockSocketPath == "" {
		return TunnelConnection{}, fmt.Errorf("%w: environment does not have tunnel connection metadata", failure.ErrFailedDependency)
	}

	connection.Port = port

	return connection, nil
}

func tunnelsFromConfig(config json.RawMessage) ([]Tunnel, error) {
	var parsed struct {
		Tunnel map[string]int `json:"tunnel,omitempty"`
	}

	decoder := json.NewDecoder(bytes.NewReader(config))
	decoder.UseNumber()

	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("parse template config: %w", err)
	}

	entries := make([]Tunnel, 0, len(parsed.Tunnel))
	for name, port := range parsed.Tunnel {
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("tunnel %s port %d is out of range", name, port)
		}

		entries = append(entries, Tunnel{Name: name, Port: port})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}
