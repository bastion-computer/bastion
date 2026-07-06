//nolint:wsl_v5 // Cluster orchestration keeps progress, remote calls, and cleanup branches adjacent.
package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/bastion-computer/bastion/core/internal/basearchive"
	"github.com/bastion-computer/bastion/core/internal/failure"
	"github.com/bastion-computer/bastion/core/internal/services"
	"github.com/bastion-computer/bastion/core/internal/services/base"
)

const clusterBaseID = 1

// BuildBase builds the singleton base on one node, stores it, and syncs it to every other node.
//
//nolint:gocyclo // Coordinates cluster preflight, node build/export, archive validation/storage, and fan-out sync.
func (s *Service) BuildBase(ctx context.Context, req base.BuildRequest) (base.Base, error) {
	if err := writeClusterProgress(req.Logs, "checking base archive storage"); err != nil {
		return base.Base{}, err
	}

	if err := s.requireArchiveStore(); err != nil {
		return base.Base{}, err
	}

	if !req.Force {
		if _, err := s.getBaseRecord(ctx); err == nil {
			return base.Base{}, fmt.Errorf("%w: base already exists", failure.ErrConflict)
		} else if !errors.Is(err, failure.ErrNotFound) {
			return base.Base{}, err
		}
	}

	if err := writeClusterProgress(req.Logs, "selecting cluster node"); err != nil {
		return base.Base{}, err
	}

	node, err := s.selectNode(ctx)
	if err != nil {
		return base.Base{}, err
	}

	if err := writeClusterProgress(req.Logs, "building base on cluster node %s", node.ID); err != nil {
		return base.Base{}, err
	}

	built, err := s.nodeClient.BuildBase(ctx, node.URL, base.BuildRequest{Force: req.Force, Logs: req.Logs})
	if err != nil {
		return base.Base{}, fmt.Errorf("%w: build base on cluster node %s: %w", failure.ErrFailedDependency, node.ID, err)
	}

	archive, err := createTempFile("bastion-cluster-base-build-*.tar.zst")
	if err != nil {
		return base.Base{}, err
	}
	defer archive.cleanup()

	if err := writeClusterProgress(req.Logs, "exporting base from cluster node %s", node.ID); err != nil {
		return base.Base{}, err
	}

	if err := s.nodeClient.ExportBase(ctx, node.URL, archive.file); err != nil {
		return base.Base{}, fmt.Errorf("%w: export base from cluster node %s: %w", failure.ErrFailedDependency, node.ID, err)
	}

	metadata, err := validateBaseArchive(ctx, archive.file)
	if err != nil {
		return base.Base{}, err
	}

	if built.ContentAddress != "" && built.ContentAddress != metadata.ContentAddress {
		return base.Base{}, fmt.Errorf("%w: built base content address does not match exported archive", failure.ErrFailedDependency)
	}

	nodes, err := s.allNodes(ctx)
	if err != nil {
		return base.Base{}, err
	}

	if err := s.importBaseArchiveToNodes(ctx, nodesExcept(nodes, node.ID), archive.path, true, metadata.ContentAddress, req.Logs); err != nil {
		return base.Base{}, err
	}

	if err := writeClusterProgress(req.Logs, "storing base archive"); err != nil {
		return base.Base{}, err
	}

	if err := putBaseArchive(ctx, s.archiveStore, archive.file); err != nil {
		return base.Base{}, err
	}

	if err := s.upsertBaseRecord(ctx, metadata, clusterBaseArchiveObjectKey()); err != nil {
		return base.Base{}, err
	}

	return metadata, nil
}

// GetBase returns the current cluster base metadata.
func (s *Service) GetBase(ctx context.Context) (base.Base, error) {
	record, err := s.getBaseRecord(ctx)
	if err != nil {
		return base.Base{}, err
	}

	return record.Metadata, nil
}

// ExportBase streams the current cluster base archive.
func (s *Service) ExportBase(ctx context.Context, archive io.Writer) error {
	if archive == nil {
		return fmt.Errorf("%w: base archive writer is required", failure.ErrInvalid)
	}

	if err := s.requireArchiveStore(); err != nil {
		return err
	}

	record, err := s.getBaseRecord(ctx)
	if err != nil {
		return err
	}

	if err := s.archiveStore.Get(ctx, record.ArchiveKey, archive); err != nil {
		return fmt.Errorf("get base archive: %w", err)
	}

	return nil
}

// ImportBase stores an uploaded base archive and syncs it to every registered node.
//
//nolint:gocyclo // Coordinates archive upload validation, node fan-out, storage, and metadata persistence.
func (s *Service) ImportBase(ctx context.Context, req base.ImportRequest) (base.Base, error) {
	if err := writeClusterProgress(req.Logs, "checking base archive storage"); err != nil {
		return base.Base{}, err
	}

	if err := s.requireArchiveStore(); err != nil {
		return base.Base{}, err
	}

	if !req.Force {
		if _, err := s.getBaseRecord(ctx); err == nil {
			return base.Base{}, fmt.Errorf("%w: base already exists", failure.ErrConflict)
		} else if !errors.Is(err, failure.ErrNotFound) {
			return base.Base{}, err
		}
	}

	if req.Archive == nil {
		return base.Base{}, fmt.Errorf("%w: base archive file is required", failure.ErrInvalid)
	}

	archive, err := createTempFile("bastion-cluster-base-import-*.tar.zst")
	if err != nil {
		return base.Base{}, err
	}
	defer archive.cleanup()

	if err := writeClusterProgress(req.Logs, "reading base archive upload"); err != nil {
		return base.Base{}, err
	}

	if _, err := io.Copy(archive.file, req.Archive); err != nil {
		return base.Base{}, fmt.Errorf("read base archive upload: %w", err)
	}

	metadata, err := validateBaseArchive(ctx, archive.file)
	if err != nil {
		return base.Base{}, err
	}

	nodes, err := s.allNodes(ctx)
	if err != nil {
		return base.Base{}, err
	}

	if err := s.importBaseArchiveToNodes(ctx, nodes, archive.path, req.Force, metadata.ContentAddress, req.Logs); err != nil {
		return base.Base{}, err
	}

	if err := writeClusterProgress(req.Logs, "storing base archive"); err != nil {
		return base.Base{}, err
	}

	if err := putBaseArchive(ctx, s.archiveStore, archive.file); err != nil {
		return base.Base{}, err
	}

	metadata.UpdatedAt = services.Now()
	if metadata.CreatedAt == "" {
		metadata.CreatedAt = metadata.UpdatedAt
	}

	if err := s.upsertBaseRecord(ctx, metadata, clusterBaseArchiveObjectKey()); err != nil {
		return base.Base{}, err
	}

	return metadata, nil
}

func (s *Service) syncBaseToNewNode(ctx context.Context, node Node, logs io.Writer) error {
	record, err := s.getBaseRecord(ctx)
	if errors.Is(err, failure.ErrNotFound) {
		return writeClusterProgress(logs, "no cluster base to sync")
	}

	if err != nil {
		return err
	}

	if err := s.requireArchiveStore(); err != nil {
		return err
	}

	archive, err := createTempFile("bastion-cluster-base-sync-*.tar.zst")
	if err != nil {
		return err
	}
	defer archive.cleanup()

	if err := writeClusterProgress(logs, "loading cluster base archive"); err != nil {
		return err
	}

	if err := s.archiveStore.Get(ctx, record.ArchiveKey, archive.file); err != nil {
		return fmt.Errorf("get base archive: %w", err)
	}

	if err := writeClusterProgress(logs, "syncing base to cluster node %s", node.ID); err != nil {
		return err
	}

	if err := importBaseArchiveToNode(ctx, s.nodeClient, node, archive.path, true, record.Metadata.ContentAddress, logs); err != nil {
		return err
	}

	return nil
}

type baseRecord struct {
	Metadata   base.Base
	ArchiveKey string
}

func (s *Service) getBaseRecord(ctx context.Context) (baseRecord, error) {
	var record baseRecord
	err := s.db.QueryRow(ctx, `SELECT content_address, archive_key, created_at, updated_at FROM cluster_base WHERE id = $1`, clusterBaseID).Scan(&record.Metadata.ContentAddress, &record.ArchiveKey, &record.Metadata.CreatedAt, &record.Metadata.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return baseRecord{}, fmt.Errorf("%w: base not found", failure.ErrNotFound)
	}

	if err != nil {
		return baseRecord{}, fmt.Errorf("get cluster base: %w", err)
	}

	return record, nil
}

func (s *Service) upsertBaseRecord(ctx context.Context, metadata base.Base, archiveKey string) error {
	_, err := s.db.Exec(ctx, `INSERT INTO cluster_base (id, content_address, archive_key, created_at, updated_at) VALUES ($1, $2, $3, $4, $5) ON CONFLICT (id) DO UPDATE SET content_address = EXCLUDED.content_address, archive_key = EXCLUDED.archive_key, created_at = EXCLUDED.created_at, updated_at = EXCLUDED.updated_at`, clusterBaseID, metadata.ContentAddress, archiveKey, metadata.CreatedAt, metadata.UpdatedAt)
	if err != nil {
		return fmt.Errorf("record cluster base: %w", err)
	}

	return nil
}

func validateBaseArchive(ctx context.Context, file *os.File) (base.Base, error) {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return base.Base{}, fmt.Errorf("rewind base archive: %w", err)
	}

	metadata, err := basearchive.Read(ctx, file)
	if err != nil {
		if errors.Is(err, basearchive.ErrInvalid) {
			return base.Base{}, fmt.Errorf("%w: import base archive: %w", failure.ErrInvalid, err)
		}

		return base.Base{}, fmt.Errorf("import base archive: %w", err)
	}

	return metadata, nil
}

func putBaseArchive(ctx context.Context, store TemplateArchiveStore, file *os.File) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind base archive: %w", err)
	}

	if err := store.Put(ctx, clusterBaseArchiveObjectKey(), file); err != nil {
		return fmt.Errorf("store base archive: %w", err)
	}

	return nil
}

func (s *Service) importBaseArchiveToNodes(ctx context.Context, nodes []Node, archivePath string, force bool, contentAddress string, logs io.Writer) error {
	if len(nodes) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(nodes))

	for _, node := range nodes {
		wg.Go(func() {
			errs <- importBaseArchiveToNode(ctx, s.nodeClient, node, archivePath, force, contentAddress, logs)
		})
	}

	wg.Wait()
	close(errs)

	var joined error
	for err := range errs {
		if err != nil {
			joined = errors.Join(joined, err)
		}
	}

	if joined != nil {
		return fmt.Errorf("%w: sync base to cluster nodes: %w", failure.ErrFailedDependency, joined)
	}

	return nil
}

func importBaseArchiveToNode(ctx context.Context, client NodeClient, node Node, archivePath string, force bool, contentAddress string, logs io.Writer) error {
	if err := writeClusterProgress(logs, "importing base on cluster node %s", node.ID); err != nil {
		return err
	}

	archive, err := os.Open(archivePath) //nolint:gosec // Path is a cluster-service temporary archive.
	if err != nil {
		return fmt.Errorf("open base archive for node %s: %w", node.ID, err)
	}
	defer func() { _ = archive.Close() }()

	info, err := archive.Stat()
	if err != nil {
		return fmt.Errorf("stat base archive for node %s: %w", node.ID, err)
	}

	imported, err := client.ImportBase(ctx, node.URL, base.ImportRequest{Force: force, Archive: archive, ArchiveSize: info.Size(), Logs: logs})
	if err != nil {
		return fmt.Errorf("import base on cluster node %s: %w", node.ID, err)
	}

	if contentAddress != "" && imported.ContentAddress != contentAddress {
		return fmt.Errorf("cluster node %s imported base content address %s, want %s", node.ID, imported.ContentAddress, contentAddress)
	}

	return nil
}

func nodesExcept(nodes []Node, id string) []Node {
	out := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if node.ID != id {
			out = append(out, node)
		}
	}

	return out
}

func clusterBaseArchiveObjectKey() string {
	return "base/base.tar.zst"
}
