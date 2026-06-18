package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/bastion-computer/bastion/core/internal/failure"
)

var templateExpression = regexp.MustCompile(`\$\{\{\s*([A-Za-z][A-Za-z0-9_]*)\.([^}\s]+)\s*\}\}`)

// TemplateSecretResolver returns the value for a template secret reference.
type TemplateSecretResolver func(context.Context, string) (string, error)

// SubstituteTemplateSecrets resolves ${{ secret.KEY }} and ${{ secret.ID }} expressions in template JSON strings.
func SubstituteTemplateSecrets(ctx context.Context, config json.RawMessage, resolve TemplateSecretResolver) (json.RawMessage, error) {
	var value any

	decoder := json.NewDecoder(bytes.NewReader(config))
	decoder.UseNumber()

	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: parse template config: %w", failure.ErrInvalid, err)
	}

	resolved, err := substituteTemplateSecretValue(ctx, value, resolve)
	if err != nil {
		return nil, err
	}

	contents, err := json.Marshal(resolved)
	if err != nil {
		return nil, fmt.Errorf("resolve template config: %w", err)
	}

	return json.RawMessage(contents), nil
}

func substituteTemplateSecretValue(ctx context.Context, value any, resolve TemplateSecretResolver) (any, error) {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			resolved, err := substituteTemplateSecretValue(ctx, child, resolve)
			if err != nil {
				return nil, err
			}

			value[key] = resolved
		}

		return value, nil
	case []any:
		for index, child := range value {
			resolved, err := substituteTemplateSecretValue(ctx, child, resolve)
			if err != nil {
				return nil, err
			}

			value[index] = resolved
		}

		return value, nil
	case string:
		return substituteTemplateSecretString(ctx, value, resolve)
	default:
		return value, nil
	}
}

func substituteTemplateSecretString(ctx context.Context, value string, resolve TemplateSecretResolver) (string, error) {
	matches := templateExpression.FindAllStringSubmatchIndex(value, -1)
	if len(matches) == 0 {
		return value, nil
	}

	var builder strings.Builder

	last := 0

	for _, match := range matches {
		namespace := value[match[2]:match[3]]
		reference := value[match[4]:match[5]]

		if namespace != "secret" {
			return "", fmt.Errorf("%w: unsupported template expression %s.%s", failure.ErrInvalid, namespace, reference)
		}

		secretValue, err := resolve(ctx, reference)
		if err != nil {
			return "", err
		}

		builder.WriteString(value[last:match[0]])
		builder.WriteString(secretValue)

		last = match[1]
	}

	builder.WriteString(value[last:])

	return builder.String(), nil
}
