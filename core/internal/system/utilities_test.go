package system

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmInstallUtilitiesAcceptsYes(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	confirmed, err := confirmInstallUtilities(false, strings.NewReader("yes\n"), &out, []string{utilityUnsquashfs})
	if err != nil {
		t.Fatalf("confirm install: %v", err)
	}

	if !confirmed {
		t.Fatal("confirmed = false, want true")
	}

	if !strings.Contains(out.String(), "missing utilities: unsquashfs") {
		t.Fatalf("prompt output = %q", out.String())
	}
}

func TestPackagesForUtilitiesDeduplicatesPackages(t *testing.T) {
	t.Parallel()

	packages, err := packagesForUtilities(packageManagerApt, []string{utilityMkfsExt4, utilityE2fsck})
	if err != nil {
		t.Fatalf("packages for utilities: %v", err)
	}

	if len(packages) != 1 || packages[0] != packageE2fsprogs {
		t.Fatalf("packages = %#v, want e2fsprogs once", packages)
	}
}
