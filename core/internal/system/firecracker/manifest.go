package firecracker

// Manifest records Firecracker assets installed under the Bastion data directory.
type Manifest struct {
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

// Assets describes installed Firecracker asset paths.
type Assets struct {
	Firecracker    string
	Jailer         string
	Kernel         string
	RootFSSquashfs string
	RootFSExt4     string
	SSHKey         string
}
