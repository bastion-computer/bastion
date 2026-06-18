//go:build darwin

// Package secret provides secret API types for the macOS client.
package secret

// Secret contains a secret and its value.
type Secret struct {
	ID        string  `json:"id"`
	Key       *string `json:"key,omitempty"`
	Value     string  `json:"value"`
	CreatedAt string  `json:"createdAt"`
}

// Metadata describes a secret without its value.
type Metadata struct {
	ID        string  `json:"id"`
	Key       *string `json:"key,omitempty"`
	CreatedAt string  `json:"createdAt"`
}

// CreateRequest contains the fields needed to create a secret.
type CreateRequest struct {
	Key   *string `json:"key,omitempty"`
	Value string  `json:"value"`
}
