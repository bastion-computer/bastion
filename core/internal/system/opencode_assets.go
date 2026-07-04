package system

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bastion-computer/bastion/core/internal/opencodeasset"
)

type openCodeAssets struct {
	openCode string
	archive  string
}

type openCodeStore struct {
	dir  string
	stat func(string) (os.FileInfo, error)
}

func newOpenCodeStore(dataDir string) openCodeStore {
	return openCodeStore{dir: opencodeasset.StoreDir(dataDir), stat: os.Stat}
}

func (s openCodeStore) ensure() error {
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return fmt.Errorf("create opencode data directory: %w", err)
	}

	return nil
}

func (s openCodeStore) assetsNode() Node {
	manifest := s.readManifest()
	assets := s.assets(manifest)

	return Node{
		Name: "assets",
		Children: []Node{
			{Name: versionedAssetName("opencode binary", opencodeasset.Version), OK: s.pinnedExecutable(manifest, assets.openCode)},
		},
	}
}

func (s openCodeStore) assets(manifest opencodeasset.Manifest) openCodeAssets {
	return openCodeAssets{
		openCode: opencodeasset.ResolveAsset(s.dir, manifest.OpenCode),
		archive:  opencodeasset.ResolveAsset(s.dir, manifest.Archive),
	}
}

func (s openCodeStore) readManifest() opencodeasset.Manifest {
	contents, err := os.ReadFile(filepath.Join(s.dir, opencodeasset.ManifestFileName))
	if err != nil {
		return opencodeasset.Manifest{}
	}

	var manifest opencodeasset.Manifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return opencodeasset.Manifest{}
	}

	return manifest
}

func (s openCodeStore) writeManifest(manifest opencodeasset.Manifest) error {
	contents, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	contents = append(contents, '\n')

	return os.WriteFile(filepath.Join(s.dir, opencodeasset.ManifestFileName), contents, 0o600)
}

func (s openCodeStore) remove() error {
	return os.RemoveAll(s.dir)
}

func (s openCodeStore) pinnedExecutable(manifest opencodeasset.Manifest, path string) bool {
	return manifest.Version == opencodeasset.Version && manifest.Architecture == archX8664 && manifest.OpenCode != "" && s.executable(path) && s.archiveAvailable(manifest)
}

func (s openCodeStore) archiveAvailable(manifest opencodeasset.Manifest) bool {
	if manifest.Archive == "" {
		return true
	}

	return s.regularFile(opencodeasset.ResolveAsset(s.dir, manifest.Archive))
}

func (s openCodeStore) regularFile(path string) bool {
	if path == "" {
		return false
	}

	info, err := s.stat(path)

	return err == nil && info.Mode().IsRegular()
}

func (s openCodeStore) executable(path string) bool {
	if !s.regularFile(path) {
		return false
	}

	info, err := s.stat(path)

	return err == nil && info.Mode().Perm()&0o111 != 0
}
