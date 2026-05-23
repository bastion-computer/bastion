package cloudhypervisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareCloudInitUsesEnvironmentIDForHostname(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sshKeyPath := filepath.Join(dir, "id_rsa")
	if err := os.WriteFile(sshKeyPath+".pub", []byte("ssh-ed25519 test-key\n"), 0o600); err != nil {
		t.Fatalf("write ssh public key: %v", err)
	}

	manager := Manager{run: func(context.Context, string, ...string) error { return nil }}
	workspace := workspace{
		dir:      dir,
		seedPath: filepath.Join(dir, envSeedFileName),
		assets:   assets{sshKey: sshKeyPath},
	}
	plan := networkPlan{guestMAC: "06:00:0A:F1:00:02", guestCIDR: "10.241.0.2/30", hostIP: "10.241.0.1"}

	const environmentID = "env_hostname"
	if err := manager.prepareCloudInit(context.Background(), environmentID, workspace, plan); err != nil {
		t.Fatalf("prepare cloud-init: %v", err)
	}

	contents, err := os.ReadFile(filepath.Join(dir, "meta-data"))
	if err != nil {
		t.Fatalf("read meta-data: %v", err)
	}

	vmID := shortID(environmentID)
	want := "instance-id: " + vmID + "\nlocal-hostname: bastion-" + strings.TrimPrefix(vmID, "vm-") + "\n"
	if string(contents) != want {
		t.Fatalf("meta-data = %q, want %q", contents, want)
	}
}
