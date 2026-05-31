package environment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

type templateFunctionConfig struct {
	Functions map[string]templateFunction `json:"functions"`
}

type templateFunction struct {
	Trigger templateFunctionTrigger `json:"trigger"`
	With    map[string]any          `json:"with,omitempty"`
}

type templateFunctionTrigger struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Key  string `json:"key,omitempty"`
}

func parseTemplateFunctions(config json.RawMessage) (map[string]templateFunction, error) {
	if len(config) == 0 {
		return nil, nil
	}

	var parsed templateFunctionConfig

	decoder := json.NewDecoder(bytes.NewReader(config))
	decoder.UseNumber()

	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("parse template functions: %w", err)
	}

	return parsed.Functions, nil
}

func (s *Service) validateFunctionQueueTriggers(ctx context.Context, config json.RawMessage) (bool, error) {
	functions, err := parseTemplateFunctions(config)
	if err != nil {
		return false, err
	}

	for name, function := range functions {
		trigger := function.Trigger
		if trigger.Type != "queue" {
			return false, fmt.Errorf("template function %s trigger type %q is unsupported", name, trigger.Type)
		}

		if _, err := s.queues.Get(ctx, trigger.ID, trigger.Key); err != nil {
			return false, fmt.Errorf("template function %s queue trigger: %w", name, err)
		}
	}

	return len(functions) > 0, nil
}
