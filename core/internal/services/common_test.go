package services_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bastion-computer/bastion/core/internal/services"
)

func TestGenerateIDReturnsPrefixedUUID(t *testing.T) {
	t.Parallel()

	value, err := services.GenerateID("env")
	if err != nil {
		t.Fatalf("generate id: %v", err)
	}

	const prefix = "env_"
	if !strings.HasPrefix(value, prefix) {
		t.Fatalf("id = %q, want prefix %q", value, prefix)
	}

	parsed, err := uuid.Parse(strings.TrimPrefix(value, prefix))
	if err != nil {
		t.Fatalf("parse uuid suffix: %v", err)
	}

	if parsed.Version() != 4 {
		t.Fatalf("uuid version = %d, want 4", parsed.Version())
	}
}

func TestGenerateIDRequiresPrefix(t *testing.T) {
	t.Parallel()

	if _, err := services.GenerateID(" "); err == nil {
		t.Fatal("generate id with empty prefix error = nil, want error")
	}
}
