// Package id generates prefixed public identifiers.
package id

import (
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Retries is the number of ID generation attempts for unique inserts.
const Retries = 5

// New returns a prefixed UUID v4 identifier.
func New(prefix string) (string, error) {
	if strings.TrimSpace(prefix) == "" {
		return "", errors.New("id prefix is required")
	}

	value, err := uuid.NewRandom()
	if err != nil {
		return "", fmt.Errorf("generate uuid: %w", err)
	}

	return prefix + "_" + value.String(), nil
}
