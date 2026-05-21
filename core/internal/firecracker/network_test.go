package firecracker

import (
	"os"
	"testing"
)

func TestPlanNetworkUsesAllocatedIndex(t *testing.T) {
	t.Parallel()

	first, err := planNetwork("env_first", 0)
	if err != nil {
		t.Fatalf("plan first network: %v", err)
	}

	if first.hostIP != "10.241.0.1" || first.guestIP != "10.241.0.2" || first.networkCIDR != "10.241.0.0/30" {
		t.Fatalf("first network = %#v", first)
	}

	second, err := planNetwork("env_second", 1)
	if err != nil {
		t.Fatalf("plan second network: %v", err)
	}

	if second.hostIP != "10.241.0.5" || second.guestIP != "10.241.0.6" || second.networkCIDR != "10.241.0.4/30" {
		t.Fatalf("second network = %#v", second)
	}

	if first.tapName == second.tapName {
		t.Fatalf("tap names should still be environment-specific: %q", first.tapName)
	}
}

func TestPlanNetworkRejectsOutOfRangeIndex(t *testing.T) {
	t.Parallel()

	if _, err := planNetwork("env_negative", -1); err == nil {
		t.Fatal("plan negative network index succeeded")
	}

	if _, err := planNetwork("env_overflow", NetworkIndexLimit); err == nil {
		t.Fatal("plan overflow network index succeeded")
	}
}

func TestReserveNetworkAllocatesAndReusesIndices(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	manager := NewManager(dataDir, 0, 0, nil)

	first := reserveNetworkForTest(t, manager, "env_first")
	if first.NetworkIndex != 0 {
		t.Fatalf("first network index = %d, want 0", first.NetworkIndex)
	}

	second := reserveNetworkForTest(t, manager, "env_second")
	if second.NetworkIndex != 1 {
		t.Fatalf("second network index = %d, want 1", second.NetworkIndex)
	}

	if err := os.RemoveAll(first.EnvDir); err != nil {
		t.Fatalf("remove first env dir: %v", err)
	}

	third := reserveNetworkForTest(t, manager, "env_third")
	if third.NetworkIndex != 0 {
		t.Fatalf("third network index = %d, want reused 0", third.NetworkIndex)
	}
}

func TestFirstFreeNetworkIndexReportsExhaustion(t *testing.T) {
	t.Parallel()

	used := make(map[int]struct{}, NetworkIndexLimit)
	for networkIndex := range NetworkIndexLimit {
		used[networkIndex] = struct{}{}
	}

	if _, ok := firstFreeNetworkIndex(used); ok {
		t.Fatal("firstFreeNetworkIndex found index in exhausted pool")
	}
}

func reserveNetworkForTest(t *testing.T, manager Manager, environmentID string) VM {
	t.Helper()

	dir := envDir(manager.DataDir, environmentID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("create env dir: %v", err)
	}

	vm, err := manager.reserveNetwork(environmentID, dir)
	if err != nil {
		t.Fatalf("reserve network: %v", err)
	}

	return vm
}
