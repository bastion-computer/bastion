package utilities

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestRegistryReportsMissingUtilities(t *testing.T) {
	t.Parallel()

	registry := Registry{
		Required: []Utility{{Name: utilityUnsquashfs}, {Name: utilitySSHKeygen}},
		LookPath: availableLookPath(utilitySSHKeygen),
	}

	missing := registry.Missing()
	if len(missing) != 1 || missing[0].Name != utilityUnsquashfs {
		t.Fatalf("missing = %#v, want unsquashfs", missing)
	}

	node := registry.Node()
	if node.Available() {
		t.Fatal("node available = true, want false")
	}
}

func availableLookPath(names ...string) func(string) (string, error) {
	available := make(map[string]bool, len(names))
	for _, name := range names {
		available[name] = true
	}

	return func(name string) (string, error) {
		if available[name] {
			return filepath.Join("/usr/bin", name), nil
		}

		return "", errors.New("not found")
	}
}
