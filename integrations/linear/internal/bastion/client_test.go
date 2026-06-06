package bastion

import "testing"

func TestShellQuote(t *testing.T) {
	t.Parallel()

	got := ShellQuote("can't stop")

	want := `'can'\''t stop'`
	if got != want {
		t.Fatalf("ShellQuote = %q, want %q", got, want)
	}
}
