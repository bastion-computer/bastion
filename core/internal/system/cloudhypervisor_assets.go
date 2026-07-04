package system

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const manifestFileName = "manifest.json"

type cloudHypervisorManifest struct {
	Version               string `json:"version"`
	UbuntuVersion         string `json:"ubuntu_version,omitempty"`
	UbuntuBuild           string `json:"ubuntu_build,omitempty"`
	Architecture          string `json:"architecture"`
	CloudHypervisor       string `json:"cloud_hypervisor"`
	Kernel                string `json:"kernel"`
	Initramfs             string `json:"initramfs"`
	RootFSImage           string `json:"rootfs_image"`
	RootFSImageType       string `json:"rootfs_image_type"`
	SSHKey                string `json:"ssh_key"`
	CreatedAt             string `json:"created_at"`
	CloudHypervisorSource string `json:"cloud_hypervisor_source,omitempty"`
	KernelSource          string `json:"kernel_source,omitempty"`
	InitramfsSource       string `json:"initramfs_source,omitempty"`
	RootFSSource          string `json:"rootfs_source,omitempty"`
	ReleaseChecksum       string `json:"release_checksum,omitempty"`
}

type cloudHypervisorAssets struct {
	cloudHypervisor        string
	cloudHypervisorVersion string
	kernel                 string
	initramfs              string
	rootFSImage            string
	ubuntuVersion          string
	sshKey                 string
}

type cloudHypervisorStore struct {
	dir  string
	stat func(string) (os.FileInfo, error)
}

func newCloudHypervisorStore(dataDir string) cloudHypervisorStore {
	return cloudHypervisorStore{dir: filepath.Join(dataDir, cloudHypervisorName), stat: os.Stat}
}

func (s cloudHypervisorStore) ensure() error {
	if err := os.MkdirAll(s.dir, 0o750); err != nil {
		return fmt.Errorf("create cloud-hypervisor data directory: %w", err)
	}

	return nil
}

func (s cloudHypervisorStore) assetsNode() Node {
	assets := s.assets()

	return Node{
		Name: "assets",
		Children: []Node{
			{Name: versionedAssetName("cloud-hypervisor binary", assets.cloudHypervisorVersion), OK: s.executable(assets.cloudHypervisor)},
			{Name: versionedAssetName("guest kernel", assets.ubuntuVersion), OK: s.regularFile(assets.kernel)},
			{Name: versionedAssetName("guest initramfs", assets.ubuntuVersion), OK: s.regularFile(assets.initramfs)},
			{Name: versionedAssetName("guest rootfs image", assets.ubuntuVersion), OK: s.regularFile(assets.rootFSImage)},
			{Name: "SSH key", OK: s.regularFile(assets.sshKey)},
		},
	}
}

func (s cloudHypervisorStore) assets() cloudHypervisorAssets {
	manifest := s.readManifest()

	return cloudHypervisorAssets{
		cloudHypervisor:        s.firstAsset(s.relativeAsset(manifest.CloudHypervisor), cloudHypervisorName),
		cloudHypervisorVersion: manifest.firstCloudHypervisorVersion(),
		kernel:                 s.firstAsset(s.relativeAsset(manifest.Kernel), "ubuntu-*-vmlinuz-*"),
		initramfs:              s.firstAsset(s.relativeAsset(manifest.Initramfs), "ubuntu-*-initrd-*"),
		rootFSImage:            s.firstAsset(s.relativeAsset(manifest.RootFSImage), "ubuntu-*.img"),
		ubuntuVersion:          manifest.firstUbuntuVersion(),
		sshKey:                 s.firstAsset(s.relativeAsset(manifest.SSHKey), "*.id_rsa"),
	}
}

func versionedAssetName(name, version string) string {
	if version == "" {
		return name
	}

	return fmt.Sprintf("%s (%s)", name, version)
}

func (m cloudHypervisorManifest) firstCloudHypervisorVersion() string {
	if m.Version != "" {
		return m.Version
	}

	return cloudHypervisorVersion
}

func (m cloudHypervisorManifest) firstUbuntuVersion() string {
	if m.UbuntuVersion == "" {
		return ubuntuNobleAssetVersion
	}

	if m.UbuntuBuild == "" {
		return m.UbuntuVersion
	}

	return m.UbuntuVersion + " " + m.UbuntuBuild
}

func (s cloudHypervisorStore) readManifest() cloudHypervisorManifest {
	contents, err := os.ReadFile(filepath.Join(s.dir, manifestFileName))
	if err != nil {
		return cloudHypervisorManifest{}
	}

	var manifest cloudHypervisorManifest
	if err := json.Unmarshal(contents, &manifest); err != nil {
		return cloudHypervisorManifest{}
	}

	return manifest
}

func (s cloudHypervisorStore) writeManifest(manifest cloudHypervisorManifest) error {
	contents, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}

	contents = append(contents, '\n')

	return os.WriteFile(filepath.Join(s.dir, manifestFileName), contents, 0o600)
}

func (s cloudHypervisorStore) remove() error {
	return os.RemoveAll(s.dir)
}

func (s cloudHypervisorStore) firstAsset(preferred, pattern string) string {
	if s.regularFile(preferred) {
		return preferred
	}

	matches, err := filepath.Glob(filepath.Join(s.dir, pattern))
	if err != nil || len(matches) == 0 {
		return ""
	}

	return matches[len(matches)-1]
}

func (s cloudHypervisorStore) relativeAsset(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(s.dir, path)
}

func (s cloudHypervisorStore) regularFile(path string) bool {
	if path == "" {
		return false
	}

	info, err := s.stat(path)

	return err == nil && info.Mode().IsRegular()
}

func (s cloudHypervisorStore) executable(path string) bool {
	if !s.regularFile(path) {
		return false
	}

	info, err := s.stat(path)

	return err == nil && info.Mode().Perm()&0o111 != 0
}
