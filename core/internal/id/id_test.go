package id_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/bastion-computer/bastion/core/internal/id"
)

func TestNewReturnsPrefixedUUID(t *testing.T) {
	t.Parallel()

	value, err := id.New("sec")
	if err != nil {
		t.Fatalf("new id: %v", err)
	}

	const prefix = "sec_"
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

func TestNewRequiresPrefix(t *testing.T) {
	t.Parallel()

	if _, err := id.New(" "); err == nil {
		t.Fatal("new id with empty prefix error = nil, want error")
	}
}
