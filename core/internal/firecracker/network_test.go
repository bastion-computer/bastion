package firecracker

import "testing"

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
