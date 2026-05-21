// Package schema embeds JSON Schema documents and validates user input.
package schema

import (
	"bytes"
	"embed"
	"fmt"
	"sync"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed template.json
var files embed.FS

const templateSchemaResource = "https://bastion.computer/schemas/template.json"

var (
	templateSchemaOnce sync.Once
	templateSchema     *jsonschema.Schema
	errTemplateSchema  error
)

// ValidateTemplateConfig validates config against the embedded template schema.
func ValidateTemplateConfig(config []byte) error {
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(config))
	if err != nil {
		return fmt.Errorf("parse template config: %w", err)
	}

	validator, err := compiledTemplateSchema()
	if err != nil {
		return err
	}

	if err := validator.Validate(value); err != nil {
		return err
	}

	return nil
}

func compiledTemplateSchema() (*jsonschema.Schema, error) {
	templateSchemaOnce.Do(func() {
		contents, err := files.ReadFile("template.json")
		if err != nil {
			errTemplateSchema = fmt.Errorf("read template schema: %w", err)

			return
		}

		document, err := jsonschema.UnmarshalJSON(bytes.NewReader(contents))
		if err != nil {
			errTemplateSchema = fmt.Errorf("parse template schema: %w", err)

			return
		}

		compiler := jsonschema.NewCompiler()
		if err := compiler.AddResource(templateSchemaResource, document); err != nil {
			errTemplateSchema = fmt.Errorf("load template schema: %w", err)

			return
		}

		templateSchema, errTemplateSchema = compiler.Compile(templateSchemaResource)
		if errTemplateSchema != nil {
			errTemplateSchema = fmt.Errorf("compile template schema: %w", errTemplateSchema)
		}
	})

	return templateSchema, errTemplateSchema
}
