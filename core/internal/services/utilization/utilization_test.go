package utilization

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
	"github.com/bastion-computer/bastion/core/internal/database"
)

const testGiB = int64(1 << 30)

func TestServiceReportsUtilizationForLiveEnvironments(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	ctx := context.Background()

	insertTemplate(t, db, "tpl_creating", `{"agents":{"opencode":{}},"resources":{"vcpu":2,"memory":3,"volume":4},"actions":{"init":[]}}`)
	insertTemplate(t, db, "tpl_running", `{"agents":{"opencode":{}},"resources":{"vcpu":1,"memory":2,"volume":8},"actions":{"init":[]}}`)
	insertTemplate(t, db, "tpl_paused", `{"agents":{"opencode":{}},"resources":{"vcpu":4,"memory":5,"volume":6},"actions":{"init":[]}}`)
	insertTemplate(t, db, "tpl_stopped", `{"agents":{"opencode":{}},"resources":{"vcpu":10,"memory":10,"volume":10},"actions":{"init":[]}}`)
	insertTemplate(t, db, "tpl_error", `{"agents":{"opencode":{}},"resources":{"vcpu":10,"memory":10,"volume":10},"actions":{"init":[]}}`)
	insertTemplate(t, db, "tpl_removed", `{"agents":{"opencode":{}},"resources":{"vcpu":10,"memory":10,"volume":10},"actions":{"init":[]}}`)

	insertEnvironment(t, db, "env_creating", ch.StateCreating, "tpl_creating")
	insertEnvironment(t, db, "env_running", ch.StateRunning, "tpl_running")
	insertEnvironment(t, db, "env_paused", ch.StatePaused, "tpl_paused")
	insertEnvironment(t, db, "env_stopped", ch.StateStopped, "tpl_stopped")
	insertEnvironment(t, db, "env_error", ch.StateError, "tpl_error")
	insertEnvironment(t, db, "env_removed", "removed", "tpl_removed")

	service := NewService(db, WithHostCapacityProvider(func(context.Context) (HostCapacity, error) {
		return HostCapacity{VCPU: 16, MemoryBytes: 64 * testGiB, VolumeBytes: 100 * testGiB}, nil
	}))

	got, err := service.Get(ctx)
	if err != nil {
		t.Fatalf("get utilization: %v", err)
	}

	want := Utilization{
		VCPU:   Resource{Total: 16, Used: 7, Available: 9},
		Memory: Resource{Total: 64 * testGiB, Used: 10 * testGiB, Available: 54 * testGiB},
		Volume: Resource{Total: 100 * testGiB, Used: 18 * testGiB, Available: 82 * testGiB},
	}
	if got != want {
		t.Fatalf("utilization = %#v, want %#v", got, want)
	}
}

func TestServiceCountsLiveVMRowsWhenEnvironmentStatusIsStale(t *testing.T) {
	t.Parallel()

	db := openDB(t)
	ctx := context.Background()

	insertTemplate(t, db, "tpl_stale", `{"agents":{"opencode":{}},"resources":{"vcpu":1,"memory":2,"volume":8},"actions":{"init":[]}}`)
	insertEnvironment(t, db, "env_stale", ch.StateStopped, "tpl_stale")
	insertEnvironmentVM(t, db, "env_stale", ch.StateRunning)

	service := NewService(db, WithHostCapacityProvider(func(context.Context) (HostCapacity, error) {
		return HostCapacity{VCPU: 4, MemoryBytes: 16 * testGiB, VolumeBytes: 100 * testGiB}, nil
	}))

	got, err := service.Get(ctx)
	if err != nil {
		t.Fatalf("get utilization: %v", err)
	}

	want := Utilization{
		VCPU:   Resource{Total: 4, Used: 1, Available: 3},
		Memory: Resource{Total: 16 * testGiB, Used: 2 * testGiB, Available: 14 * testGiB},
		Volume: Resource{Total: 100 * testGiB, Used: 8 * testGiB, Available: 92 * testGiB},
	}
	if got != want {
		t.Fatalf("utilization = %#v, want %#v", got, want)
	}
}

func TestDetectVCPUThreadsUsesCPUTopologyFormula(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for cpu, topology := range []struct {
		physicalPackage int
		core            int
	}{
		{physicalPackage: 0, core: 0},
		{physicalPackage: 0, core: 0},
		{physicalPackage: 0, core: 1},
		{physicalPackage: 0, core: 1},
		{physicalPackage: 1, core: 0},
		{physicalPackage: 1, core: 0},
		{physicalPackage: 1, core: 1},
		{physicalPackage: 1, core: 1},
	} {
		writeCPUTopology(t, dir, cpu, topology.physicalPackage, topology.core)
	}

	got, err := detectVCPUThreads(dir)
	if err != nil {
		t.Fatalf("detect vCPU threads: %v", err)
	}

	if got != 8 {
		t.Fatalf("vCPU threads = %d, want 8", got)
	}
}

func TestParseMemTotalBytes(t *testing.T) {
	t.Parallel()

	got, err := parseMemTotalBytes([]byte("MemFree: 2048 kB\nMemTotal: 12345 kB\n"))
	if err != nil {
		t.Fatalf("parse meminfo: %v", err)
	}

	if got != 12345*1024 {
		t.Fatalf("MemTotal bytes = %d, want %d", got, 12345*1024)
	}
}

func openDB(t *testing.T) *database.Client {
	t.Helper()

	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return db
}

func insertTemplate(t *testing.T, db *database.Client, id, config string) {
	t.Helper()

	_, err := db.ExecContext(context.Background(), `INSERT INTO templates (id, key, config, created_at) VALUES (?, ?, ?, ?)`, id, nil, config, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("insert template %s: %v", id, err)
	}
}

func insertEnvironment(t *testing.T, db *database.Client, id, status, templateID string) {
	t.Helper()

	_, err := db.ExecContext(context.Background(), `INSERT INTO environments (id, key, status, template_id, created_at, updated_at, last_error) VALUES (?, ?, ?, ?, ?, ?, ?)`, id, nil, status, templateID, "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", "")
	if err != nil {
		t.Fatalf("insert environment %s: %v", id, err)
	}
}

func insertEnvironmentVM(t *testing.T, db *database.Client, environmentID, state string) {
	t.Helper()

	_, err := db.ExecContext(context.Background(), `
INSERT INTO environment_vms (
  environment_id, vm_id, state, pid, env_dir, runtime_dir, socket_path, vsock_socket_path, kernel_path, rootfs_path,
  tap_name, host_ip, guest_ip, guest_cidr, guest_mac, ssh_user, ssh_port, ssh_key_path,
  created_at, updated_at, last_error
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, environmentID, "vm_"+environmentID, state, 1234, "/tmp/env", "/run/env", "/run/env/api.sock", "/run/env/vsock.sock", "/tmp/kernel", "/tmp/rootfs", "tap0", "10.0.0.1", "10.0.0.2", "10.0.0.2/30", "02:00:00:00:00:01", ch.SSHUser, ch.SSHPort, "/tmp/key", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", "")
	if err != nil {
		t.Fatalf("insert environment vm %s: %v", environmentID, err)
	}
}

func writeCPUTopology(t *testing.T, root string, cpu, physicalPackage, core int) {
	t.Helper()

	topologyDir := filepath.Join(root, "cpu"+strconv.Itoa(cpu), "topology")
	if err := os.MkdirAll(topologyDir, 0o750); err != nil {
		t.Fatalf("create CPU topology: %v", err)
	}

	if err := os.WriteFile(filepath.Join(topologyDir, "physical_package_id"), []byte(strconv.Itoa(physicalPackage)+"\n"), 0o600); err != nil {
		t.Fatalf("write physical package: %v", err)
	}

	if err := os.WriteFile(filepath.Join(topologyDir, "core_id"), []byte(strconv.Itoa(core)+"\n"), 0o600); err != nil {
		t.Fatalf("write core id: %v", err)
	}
}
