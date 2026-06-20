package clusterapi

import (
	"context"
	"fmt"
	"sync"

	"github.com/bastion-computer/bastion/core/internal/failure"
)

// ArchiveStore stores exported source template archives.
type ArchiveStore interface {
	Put(context.Context, string, []byte) error
	Get(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}

// MemoryArchiveStore stores template archives in memory.
type MemoryArchiveStore struct {
	mu       sync.Mutex
	archives map[string][]byte
}

// NewMemoryArchiveStore returns an empty archive store.
func NewMemoryArchiveStore() *MemoryArchiveStore {
	return &MemoryArchiveStore{archives: make(map[string][]byte)}
}

// Put stores archive bytes.
func (s *MemoryArchiveStore) Put(_ context.Context, key string, contents []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.archives[key] = append([]byte(nil), contents...)

	return nil
}

// Get returns archive bytes.
func (s *MemoryArchiveStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	contents, ok := s.archives[key]
	if !ok {
		return nil, fmt.Errorf("%w: template archive not found", failure.ErrNotFound)
	}

	return append([]byte(nil), contents...), nil
}

// Delete removes archive bytes.
func (s *MemoryArchiveStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.archives, key)

	return nil
}
