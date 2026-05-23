package system

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfirmInstallUtilitiesAcceptsYes(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	confirmed, err := confirmInstallUtilities(false, strings.NewReader("yes\n"), &out, []string{utilityQEMUImg})
	if err != nil {
		t.Fatalf("confirm install: %v", err)
	}

	if !confirmed {
		t.Fatal("confirmed = false, want true")
	}

	if !strings.Contains(out.String(), "missing utilities: qemu-img") {
		t.Fatalf("prompt output = %q", out.String())
	}
}

func TestPackagesForUtilitiesDeduplicatesPackages(t *testing.T) {
	t.Parallel()

	packages, err := packagesForUtilities(packageManagerApt, []string{utilitySSHKeygen, utilitySSH, utilitySCP})
	if err != nil {
		t.Fatalf("packages for utilities: %v", err)
	}

	if len(packages) != 1 || packages[0] != packageOpenSSHDeb {
		t.Fatalf("packages = %#v, want openssh-client once", packages)
	}
}
