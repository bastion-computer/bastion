package environment

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
	"github.com/bastion-computer/bastion/core/internal/failure"
)

// AgentConnection contains private connection metadata for API-managed agent proxying.
type AgentConnection struct {
	Host string
	Port int
}

// AgentConnection returns private HTTP connection metadata for an environment agent.
func (s *Service) AgentConnection(ctx context.Context, environmentID, agentName string) (AgentConnection, error) {
	if agentName != ch.AgentOpenCode {
		return AgentConnection{}, fmt.Errorf("%w: environment agent %s not found", failure.ErrNotFound, agentName)
	}

	environment, err := s.Get(ctx, environmentID)
	if err != nil {
		return AgentConnection{}, err
	}

	if environment.Status != ch.StateRunning {
		return AgentConnection{}, fmt.Errorf("%w: environment status is %q, want running", failure.ErrFailedDependency, environment.Status)
	}

	record, err := s.getRecord(ctx, environment.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentConnection{}, fmt.Errorf("%w: environment not found", failure.ErrNotFound)
	}

	if err != nil {
		return AgentConnection{}, fmt.Errorf("get environment agent metadata: %w", err)
	}

	if record.Host == "" {
		return AgentConnection{}, fmt.Errorf("%w: environment does not have agent connection metadata", failure.ErrFailedDependency)
	}

	config, err := s.templateConfig(ctx, environment.TemplateID)
	if err != nil {
		return AgentConnection{}, err
	}

	port, err := opencodeAgentPort(config)
	if err != nil {
		return AgentConnection{}, fmt.Errorf("%w: %w", failure.ErrFailedDependency, err)
	}

	return AgentConnection{Host: record.Host, Port: port}, nil
}

func (s *Service) templateConfig(ctx context.Context, templateID string) (json.RawMessage, error) {
	var config string

	err := s.db.QueryRowContext(ctx, `SELECT config FROM templates WHERE id = ?`, templateID).Scan(&config)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: template not found", failure.ErrNotFound)
	}

	if err != nil {
		return nil, fmt.Errorf("get environment template config: %w", err)
	}

	return json.RawMessage(config), nil
}

func opencodeAgentPort(config json.RawMessage) (int, error) {
	var parsed struct {
		Agents struct {
			OpenCode *struct {
				Config map[string]any `json:"config,omitempty"`
			} `json:"opencode,omitempty"`
		} `json:"agents"`
	}

	decoder := json.NewDecoder(bytes.NewReader(config))
	decoder.UseNumber()

	if err := decoder.Decode(&parsed); err != nil {
		return 0, fmt.Errorf("parse template config: %w", err)
	}

	if parsed.Agents.OpenCode == nil {
		return 0, fmt.Errorf("environment agent %s not found", ch.AgentOpenCode)
	}

	return opencodePortFromConfig(parsed.Agents.OpenCode.Config)
}

func opencodePortFromConfig(config map[string]any) (int, error) {
	if config == nil {
		return ch.OpenCodeDefaultPort, nil
	}

	serverValue, ok := config["server"]
	if !ok {
		return ch.OpenCodeDefaultPort, nil
	}

	server, ok := serverValue.(map[string]any)
	if !ok {
		return 0, errors.New("opencode config server must be an object")
	}

	portValue, ok := server["port"]
	if !ok {
		return ch.OpenCodeDefaultPort, nil
	}

	port, err := agentPortInt(portValue)
	if err != nil {
		return 0, fmt.Errorf("opencode config server port: %w", err)
	}

	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("opencode config server port %d is out of range", port)
	}

	return port, nil
}

func agentPortInt(value any) (int, error) {
	switch value := value.(type) {
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return 0, err
		}

		return int(parsed), nil
	case float64:
		if math.Trunc(value) != value {
			return 0, errors.New("must be an integer")
		}

		return int(value), nil
	case int:
		return value, nil
	case int64:
		return int(value), nil
	default:
		return 0, errors.New("must be an integer")
	}
}
