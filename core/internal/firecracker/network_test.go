package firecracker

import (
	"context"
	"os"
	"reflect"
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

func TestCleanupStaleTapCIDRRemovesOnlyStaleBastionTaps(t *testing.T) {
	t.Parallel()

	var commands []string

	manager := Manager{
		run: func(_ context.Context, name string, args ...string) error {
			commands = append(commands, commandString(name, args))

			return nil
		},
		output: func(_ context.Context, name string, args ...string) (string, error) {
			if got := commandString(name, args); got != "ip -o -4 addr show" {
				t.Fatalf("output command = %q, want ip -o -4 addr show", got)
			}

			return "1: lo    inet 127.0.0.1/8 scope host lo\n" +
				"60: btstale    inet 10.241.0.1/30 scope global btstale\n" +
				"61: eth0    inet 10.241.0.1/30 scope global eth0\n" +
				"62: btother    inet 10.241.0.5/30 scope global btother\n", nil
		},
	}

	plan := networkPlan{tapName: "btnew", hostCIDR: "10.241.0.1/30", networkCIDR: "10.241.0.0/30", hostIface: "eth0"}
	if err := manager.cleanupStaleTapCIDR(context.Background(), plan); err != nil {
		t.Fatalf("cleanup stale tap cidr: %v", err)
	}

	want := []string{
		"iptables -D FORWARD -i btstale -j ACCEPT",
		"iptables -D FORWARD -o btstale -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT",
		"iptables -t nat -D POSTROUTING -s 10.241.0.0/30 -o eth0 -j MASQUERADE",
		"ip link del btstale",
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
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
