package utilities

import (
	"bytes"
	"strings"
	"testing"
)

func TestPromptConfirmsInstallWithYesFlag(t *testing.T) {
	t.Parallel()

	confirmed, err := (Prompt{Yes: true}).ConfirmInstall([]Utility{{Name: utilityUnsquashfs}})
	if err != nil {
		t.Fatalf("confirm install: %v", err)
	}

	if !confirmed {
		t.Fatal("confirmed = false, want true")
	}
}

func TestPromptAsksBeforeInstall(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	confirmed, err := (Prompt{
		In:  strings.NewReader("n\n"),
		Out: &out,
	}).ConfirmInstall([]Utility{{Name: utilityUnsquashfs}})
	if err != nil {
		t.Fatalf("confirm install: %v", err)
	}

	if confirmed {
		t.Fatal("confirmed = true, want false")
	}

	if !strings.Contains(out.String(), "install missing utilities? [y/N]") {
		t.Fatalf("prompt output = %q", out.String())
	}
}
