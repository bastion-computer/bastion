package firecracker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bastion-computer/bastion/core/internal/system/dependencies"
)

const manifestFileName = "manifest.json"

// Store owns Firecracker files under the Bastion data directory.
type Store struct {
	Dir  string
	Stat func(string) (os.FileInfo, error)
}

// NewStore returns a Firecracker store under dataDir.
func NewStore(dataDir string) Store {
	return Store{Dir: filepath.Join(dataDir, dependencyName), Stat: os.Stat}
}

// Ensure creates the Firecracker store directory.
func (s Store) Ensure() error {
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return fmt.Errorf("create firecracker data directory: %w", err)
	}

	return nil
}

// Assets returns discovered Firecracker assets in the store.
func (s Store) Assets() Assets {
	s = s.withDefaults()
	manifest := s.ReadManifest()

	return Assets{
		Firecracker:    s.firstAsset(s.relativeAsset(manifest.Firecracker), dependencyName),
		Jailer:         s.firstAsset(s.relativeAsset(manifest.Jailer), jailerName),
		Kernel:         s.firstAsset(s.relativeAsset(manifest.Kernel), "vmlinux-*"),
		RootFSSquashfs: s.firstAsset(s.relativeAsset(manifest.RootFSSquashfs), "ubuntu-*.squashfs"),
		RootFSExt4:     s.firstAsset(s.relativeAsset(manifest.RootFSExt4), "ubuntu-*.ext4"),
		SSHKey:         s.firstAsset(s.relativeAsset(manifest.SSHKey), "*.id_rsa"),
	}
}

// AssetsNode returns the Firecracker asset dependency subtree.
func (s Store) AssetsNode() dependencies.Node {
	assets := s.Assets()

	return dependencies.Node{
		Name: "assets",
		Children: []dependencies.Node{
			{Name: "firecracker binary", OK: s.executable(assets.Firecracker)},
			{Name: "jailer binary", OK: s.executable(assets.Jailer)},
			{Name: "guest kernel", OK: s.regularFile(assets.Kernel)},
			{Name: "guest rootfs squashfs", OK: s.regularFile(assets.RootFSSquashfs)},
			{Name: "guest rootfs ext4", OK: s.regularFile(assets.RootFSExt4)},
			{Name: "SSH key", OK: s.regularFile(assets.SSHKey)},
		},
	}
}

// ReadManifest reads a Firecracker manifest from the store.
func (s Store) ReadManifest() Manifest {
	contents, err := os.ReadFile(filepath.Join(s.Dir, manifestFileName))
	if err != nil {
		return Manifest{}
	}

	var manifest Manifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return Manifest{}
	}

	return manifest
}

// WriteManifest writes a Firecracker manifest to the store.
func (s Store) WriteManifest(manifest Manifest) error {
	contents, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	contents = append(contents, '\n')

	return os.WriteFile(filepath.Join(s.Dir, manifestFileName), contents, 0o600)
}

// Remove deletes the Firecracker store directory.
func (s Store) Remove() error {
	return os.RemoveAll(s.Dir)
}

func (s Store) withDefaults() Store {
	if s.Stat == nil {
		s.Stat = os.Stat
	}

	return s
}

func (s Store) firstAsset(preferred, pattern string) string {
	if s.regularFile(preferred) {
		return preferred
	}

	matches, err := filepath.Glob(filepath.Join(s.Dir, pattern))
	if err != nil || len(matches) == 0 {
		return ""
	}

	return matches[len(matches)-1]
}

func (s Store) relativeAsset(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(s.Dir, path)
}

func (s Store) regularFile(path string) bool {
	if path == "" {
		return false
	}

	info, err := s.withDefaults().Stat(path)

	return err == nil && info.Mode().IsRegular()
}

func (s Store) executable(path string) bool {
	if !s.regularFile(path) {
		return false
	}

	info, err := s.withDefaults().Stat(path)

	return err == nil && info.Mode().Perm()&0o111 != 0
}
