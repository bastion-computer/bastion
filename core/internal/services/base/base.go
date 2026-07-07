//go:build !darwin

package base

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/bastion-computer/bastion/core/internal/basearchive"
	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services"
)

// Orchestrator manages base VM artifacts.
type Orchestrator interface {
	BuildBase(context.Context, ch.BuildBaseRequest) (basearchive.Metadata, error)
	GetBase(context.Context) (basearchive.Metadata, error)
	ExportBase(context.Context, ch.ExportBaseRequest) error
	ImportBase(context.Context, ch.ImportBaseRequest) (basearchive.Metadata, error)
}

// Option configures the base service.
type Option func(*Service)

// Service manages the singleton base resource.
type Service struct {
	orchestrator Orchestrator
}

// NewService returns a base service.
func NewService(opts ...Option) *Service {
	service := &Service{orchestrator: noopOrchestrator{}}
	for _, opt := range opts {
		opt(service)
	}

	if service.orchestrator == nil {
		service.orchestrator = noopOrchestrator{}
	}

	return service
}

// WithOrchestrator configures base VM artifact management.
func WithOrchestrator(orchestrator Orchestrator) Option {
	return func(s *Service) {
		s.orchestrator = orchestrator
	}
}

// Build creates a base image.
func (s *Service) Build(ctx context.Context, req BuildRequest) (Base, error) {
	base, err := s.orchestrator.BuildBase(ctx, ch.BuildBaseRequest{Force: req.Force, Logs: req.Logs})
	if err != nil {
		return Base{}, mapError("build base", err)
	}

	return base, nil
}

// Get returns the current base image metadata.
func (s *Service) Get(ctx context.Context) (Base, error) {
	base, err := s.orchestrator.GetBase(ctx)
	if err != nil {
		return Base{}, mapError("get base", err)
	}

	return base, nil
}

// Export streams the current base image archive.
func (s *Service) Export(ctx context.Context, archive io.Writer) error {
	if archive == nil {
		return fmt.Errorf("%w: base archive writer is required", failure.ErrInvalid)
	}

	if err := s.orchestrator.ExportBase(ctx, ch.ExportBaseRequest{Writer: archive}); err != nil {
		return mapError("export base", err)
	}

	return nil
}

// Import stores an uploaded base image archive.
func (s *Service) Import(ctx context.Context, req ImportRequest) (Base, error) {
	if req.Archive == nil {
		return Base{}, fmt.Errorf("%w: base archive file is required", failure.ErrInvalid)
	}

	base, err := s.orchestrator.ImportBase(ctx, ch.ImportBaseRequest{Force: req.Force, Reader: req.Archive, ContentLength: req.ArchiveSize, Logs: req.Logs})
	if err != nil {
		return Base{}, mapError("import base", err)
	}

	return base, nil
}

func mapError(operation string, err error) error {
	switch {
	case errors.Is(err, ch.ErrBaseExists):
		return fmt.Errorf("%w: base already exists", failure.ErrConflict)
	case errors.Is(err, ch.ErrBaseNotFound):
		return fmt.Errorf("%w: base not found", failure.ErrNotFound)
	case errors.Is(err, ch.ErrInvalidBaseArchive):
		return fmt.Errorf("%w: %s: %w", failure.ErrInvalid, operation, err)
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}

type noopOrchestrator struct{}

func (noopOrchestrator) BuildBase(_ context.Context, _ ch.BuildBaseRequest) (basearchive.Metadata, error) {
	now := services.Now()

	return basearchive.Metadata{ContentAddress: "sha256:noop", CreatedAt: now, UpdatedAt: now}, nil
}

func (noopOrchestrator) GetBase(context.Context) (basearchive.Metadata, error) {
	now := services.Now()

	return basearchive.Metadata{ContentAddress: "sha256:noop", CreatedAt: now, UpdatedAt: now}, nil
}

func (noopOrchestrator) ExportBase(_ context.Context, _ ch.ExportBaseRequest) error {
	return nil
}

func (noopOrchestrator) ImportBase(_ context.Context, _ ch.ImportBaseRequest) (basearchive.Metadata, error) {
	now := services.Now()

	return basearchive.Metadata{ContentAddress: "sha256:noop", CreatedAt: now, UpdatedAt: now}, nil
}
