package environment

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/bastion-computer/bastion/core/internal/failure"
)

var templateEnvExpression = regexp.MustCompile(`\$\{\{\s*env\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

func substituteTemplateEnvironment(config json.RawMessage) (json.RawMessage, error) {
	var value any

	decoder := json.NewDecoder(bytes.NewReader(config))
	decoder.UseNumber()

	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: parse template config: %w", failure.ErrInvalid, err)
	}

	resolved, err := substituteTemplateEnvironmentValue(value)
	if err != nil {
		return nil, err
	}

	contents, err := json.Marshal(resolved)
	if err != nil {
		return nil, fmt.Errorf("resolve template config: %w", err)
	}

	return json.RawMessage(contents), nil
}

func substituteTemplateEnvironmentValue(value any) (any, error) {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			resolved, err := substituteTemplateEnvironmentValue(child)
			if err != nil {
				return nil, err
			}

			value[key] = resolved
		}

		return value, nil
	case []any:
		for index, child := range value {
			resolved, err := substituteTemplateEnvironmentValue(child)
			if err != nil {
				return nil, err
			}

			value[index] = resolved
		}

		return value, nil
	case string:
		return substituteTemplateEnvironmentString(value)
	default:
		return value, nil
	}
}

func substituteTemplateEnvironmentString(value string) (string, error) {
	matches := templateEnvExpression.FindAllStringSubmatchIndex(value, -1)
	if len(matches) == 0 {
		return value, nil
	}

	var builder strings.Builder

	last := 0

	for _, match := range matches {
		name := value[match[2]:match[3]]

		envValue, ok := os.LookupEnv(name)
		if !ok {
			return "", fmt.Errorf("%w: environment variable %s is not set", failure.ErrInvalid, name)
		}

		builder.WriteString(value[last:match[0]])
		builder.WriteString(envValue)

		last = match[1]
	}

	builder.WriteString(value[last:])

	return builder.String(), nil
}
