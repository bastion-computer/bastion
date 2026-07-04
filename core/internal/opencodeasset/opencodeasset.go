// Package opencodeasset defines the pinned OpenCode asset layout shared by
// system setup and Cloud Hypervisor template preparation.
package opencodeasset

import "path/filepath"

const (
	// Version is the pinned OpenCode release downloaded by Bastion system setup.
	Version = "v1.17.13"
	// DirName is the Bastion data directory child containing OpenCode assets.
	DirName = "opencode"
	// BinaryName is the OpenCode executable file name.
	BinaryName = "opencode"
	// LinuxX64ArchiveName is the pinned official Linux x64 release archive.
	LinuxX64ArchiveName = "opencode-linux-x64.tar.gz"
	// ManifestFileName is the OpenCode asset manifest file name.
	ManifestFileName = "manifest.json"
)

// Manifest records the pinned OpenCode asset installed in the Bastion data directory.
type Manifest struct {
	Version      string `json:"version"`
	Architecture string `json:"architecture"`
	OpenCode     string `json:"opencode"`
	Archive      string `json:"archive,omitempty"`
	CreatedAt    string `json:"created_at"`
	Source       string `json:"source,omitempty"`
	Checksum     string `json:"checksum,omitempty"`
}

// StoreDir returns the OpenCode asset directory under the Bastion data directory.
func StoreDir(dataDir string) string {
	return filepath.Join(dataDir, DirName)
}

// ResolveAsset resolves a manifest asset path relative to its store directory.
func ResolveAsset(storeDir, assetPath string) string {
	if assetPath == "" || filepath.IsAbs(assetPath) {
		return assetPath
	}

	return filepath.Join(storeDir, assetPath)
}
