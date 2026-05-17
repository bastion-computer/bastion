package system

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestExecRunnerPrefixesCommandOutput(t *testing.T) {
	t.Parallel()

	var (
		out    bytes.Buffer
		errOut bytes.Buffer
	)

	runner := NewExecRunner(&out, &errOut)

	err := runner.Run(context.Background(), "sh", "-c", "printf 'out\\npartial'; printf 'err\\n' >&2")
	if err != nil {
		t.Fatalf("run command: %v", err)
	}

	if got := out.String(); got != "sh: out\nsh: partial" {
		t.Fatalf("stdout = %q, want prefixed output", got)
	}

	if got := errOut.String(); got != "sh: err\n" {
		t.Fatalf("stderr = %q, want prefixed output", got)
	}
}

func TestCommandOutputLabelUsesPrivilegedUtility(t *testing.T) {
	t.Parallel()

	got := commandOutputLabel(utilitySudo, []string{"/usr/sbin/mkfs.ext4", "-F", "rootfs.ext4"})
	if got != utilityMkfsExt4 {
		t.Fatalf("label = %q, want %q", got, utilityMkfsExt4)
	}
}

func TestCommandOutputPrefixerPrefixesEachLine(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	writer := newCommandOutputPrefixer(&out, "utility")

	if _, err := writer.Write([]byte("one\nt")); err != nil {
		t.Fatalf("write first chunk: %v", err)
	}

	if _, err := writer.Write([]byte("wo\n")); err != nil {
		t.Fatalf("write second chunk: %v", err)
	}

	if got := out.String(); !strings.Contains(got, "utility: one\nutility: two\n") {
		t.Fatalf("prefixed output = %q", got)
	}
}
