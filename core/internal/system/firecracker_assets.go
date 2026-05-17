package system

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const manifestFileName = "manifest.json"

type firecrackerManifest struct {
	Version         string `json:"version"`
	Architecture    string `json:"architecture"`
	Firecracker     string `json:"firecracker"`
	Jailer          string `json:"jailer"`
	Kernel          string `json:"kernel"`
	RootFSSquashfs  string `json:"rootfs_squashfs"`
	RootFSExt4      string `json:"rootfs_ext4"`
	SSHKey          string `json:"ssh_key"`
	CreatedAt       string `json:"created_at"`
	KernelSource    string `json:"kernel_source,omitempty"`
	RootFSSource    string `json:"rootfs_source,omitempty"`
	ReleaseAsset    string `json:"release_asset,omitempty"`
	ReleaseChecksum string `json:"release_checksum,omitempty"`
}

type firecrackerAssets struct {
	firecracker    string
	jailer         string
	kernel         string
	rootFSSquashfs string
	rootFSExt4     string
	sshKey         string
}

type firecrackerStore struct {
	dir  string
	stat func(string) (os.FileInfo, error)
}

func newFirecrackerStore(dataDir string) firecrackerStore {
	return firecrackerStore{dir: filepath.Join(dataDir, firecrackerName), stat: os.Stat}
}

func (s firecrackerStore) ensure() error {
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return fmt.Errorf("create firecracker data directory: %w", err)
	}

	return nil
}

func (s firecrackerStore) assetsNode() Node {
	assets := s.assets()

	return Node{
		Name: "assets",
		Children: []Node{
			{Name: "firecracker binary", OK: s.executable(assets.firecracker)},
			{Name: "jailer binary", OK: s.executable(assets.jailer)},
			{Name: "guest kernel", OK: s.regularFile(assets.kernel)},
			{Name: "guest rootfs squashfs", OK: s.regularFile(assets.rootFSSquashfs)},
			{Name: "guest rootfs ext4", OK: s.regularFile(assets.rootFSExt4)},
			{Name: "SSH key", OK: s.regularFile(assets.sshKey)},
		},
	}
}

func (s firecrackerStore) assets() firecrackerAssets {
	manifest := s.readManifest()

	return firecrackerAssets{
		firecracker:    s.firstAsset(s.relativeAsset(manifest.Firecracker), firecrackerName),
		jailer:         s.firstAsset(s.relativeAsset(manifest.Jailer), jailerName),
		kernel:         s.firstAsset(s.relativeAsset(manifest.Kernel), "vmlinux-*"),
		rootFSSquashfs: s.firstAsset(s.relativeAsset(manifest.RootFSSquashfs), "ubuntu-*.squashfs"),
		rootFSExt4:     s.firstAsset(s.relativeAsset(manifest.RootFSExt4), "ubuntu-*.ext4"),
		sshKey:         s.firstAsset(s.relativeAsset(manifest.SSHKey), "*.id_rsa"),
	}
}

func (s firecrackerStore) readManifest() firecrackerManifest {
	contents, err := os.ReadFile(filepath.Join(s.dir, manifestFileName))
	if err != nil {
		return firecrackerManifest{}
	}

	var manifest firecrackerManifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return firecrackerManifest{}
	}

	return manifest
}

func (s firecrackerStore) writeManifest(manifest firecrackerManifest) error {
	contents, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	contents = append(contents, '\n')

	return os.WriteFile(filepath.Join(s.dir, manifestFileName), contents, 0o600)
}

func (s firecrackerStore) remove() error {
	return os.RemoveAll(s.dir)
}

func (s firecrackerStore) firstAsset(preferred, pattern string) string {
	if s.regularFile(preferred) {
		return preferred
	}

	matches, err := filepath.Glob(filepath.Join(s.dir, pattern))
	if err != nil || len(matches) == 0 {
		return ""
	}

	return matches[len(matches)-1]
}

func (s firecrackerStore) relativeAsset(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(s.dir, path)
}

func (s firecrackerStore) regularFile(path string) bool {
	if path == "" {
		return false
	}

	info, err := s.stat(path)

	return err == nil && info.Mode().IsRegular()
}

func (s firecrackerStore) executable(path string) bool {
	if !s.regularFile(path) {
		return false
	}

	info, err := s.stat(path)

	return err == nil && info.Mode().Perm()&0o111 != 0
}
