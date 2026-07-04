package cloudhypervisor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bastion-computer/bastion/core/internal/opencodeasset"
)

const (
	openCodeTmpPath        = "/tmp/" + opencodeasset.BinaryName
	openCodeArchiveTmpPath = "/tmp/" + opencodeasset.LinuxX64ArchiveName
	openCodeGuestPath      = "/usr/local/bin/" + opencodeasset.BinaryName
)

type openCodeAssets struct {
	openCode string
	archive  string
}

func loadOpenCodeAssets(dataDir string) (openCodeAssets, error) {
	assetDir := opencodeasset.StoreDir(dataDir)
	manifestPath := filepath.Join(assetDir, opencodeasset.ManifestFileName)

	contents, err := os.ReadFile(manifestPath) //nolint:gosec // Path is rooted in the configured Bastion data directory.
	if err != nil {
		return openCodeAssets{}, fmt.Errorf("read opencode manifest: %w", err)
	}

	var manifest opencodeasset.Manifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return openCodeAssets{}, fmt.Errorf("parse opencode manifest: %w", err)
	}

	if manifest.Version != opencodeasset.Version {
		return openCodeAssets{}, fmt.Errorf("opencode version = %s, want %s", manifest.Version, opencodeasset.Version)
	}

	if manifest.OpenCode == "" {
		return openCodeAssets{}, fmt.Errorf("opencode asset missing from manifest: %s", opencodeasset.BinaryName)
	}

	openCodePath := opencodeasset.ResolveAsset(assetDir, manifest.OpenCode)
	if _, err := executableFile(openCodePath); err != nil {
		return openCodeAssets{}, fmt.Errorf("stat opencode asset: %w", err)
	}

	assets := openCodeAssets{openCode: openCodePath}

	if manifest.Archive != "" {
		archivePath := opencodeasset.ResolveAsset(assetDir, manifest.Archive)
		if err := regularFile(archivePath); err != nil {
			return openCodeAssets{}, fmt.Errorf("stat opencode archive: %w", err)
		}

		assets.archive = archivePath
	}

	return assets, nil
}

func regularFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}

	return nil
}
