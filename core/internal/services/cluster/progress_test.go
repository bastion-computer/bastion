package cluster

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestWriteClusterProgressPrefixesLogLine(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	if err := writeClusterProgress(&logs, "resolving namespace"); err != nil {
		t.Fatalf("write cluster progress: %v", err)
	}

	if got := logs.String(); got != "cluster: resolving namespace\n" {
		t.Fatalf("progress log = %q, want cluster-prefixed line", got)
	}
}

func TestWriteClusterProgressPropagatesWriteError(t *testing.T) {
	t.Parallel()

	err := writeClusterProgress(failingProgressWriter{err: errors.New("client disconnected")}, "selecting node")
	if err == nil || !strings.Contains(err.Error(), "client disconnected") {
		t.Fatalf("write error = %v, want client disconnected", err)
	}
}

type failingProgressWriter struct {
	err error
}

func (w failingProgressWriter) Write([]byte) (int, error) {
	return 0, w.err
}
