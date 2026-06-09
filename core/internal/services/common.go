// Package services contains shared service-layer helpers.
package services

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/google/uuid"
)

// GenerateID returns a prefixed UUID v4 identifier.
func GenerateID(prefix string) (string, error) {
	if strings.TrimSpace(prefix) == "" {
		return "", errors.New("id prefix is required")
	}

	value, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("generate uuid: %w", err)
	}

	return prefix + "_" + value.String(), nil
}

// RequireIDOrKey validates that exactly one resource identifier is set.
func RequireIDOrKey(id, key string) error {
	if (id == "") == (key == "") {
		return fmt.Errorf("%w: specify exactly one of id or key", failure.ErrInvalid)
	}

	return nil
}

// ValidateOptionalKey rejects explicitly provided blank resource keys.
func ValidateOptionalKey(resource string, key *string) error {
	if key != nil && strings.TrimSpace(*key) == "" {
		return fmt.Errorf("%w: %s key cannot be blank", failure.ErrInvalid, resource)
	}

	return nil
}

// OptionalStringValue returns a database value for an optional string.
func OptionalStringValue(value *string) any {
	if value == nil {
		return nil
	}

	return *value
}

// NullStringPtr returns a string pointer for a nullable database string.
func NullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}

	return CopyStringPtr(&value.String)
}

// CopyStringPtr returns a copy of an optional string pointer.
func CopyStringPtr(value *string) *string {
	if value == nil {
		return nil
	}

	copied := *value

	return &copied
}

// LookupClause returns the WHERE clause and value for an ID-or-key lookup.
func LookupClause(id, key, idColumn, keyColumn string) (string, any) {
	if id != "" {
		return idColumn + " = ?", id
	}

	return keyColumn + " = ?", key
}

// Now returns the canonical service timestamp string.
func Now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}
