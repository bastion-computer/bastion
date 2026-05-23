package system

import (
	"bytes"
	"strings"
	"testing"
)

func TestNodeRenderAggregatesChildStatus(t *testing.T) {
	t.Parallel()

	depTree := Node{
		Name: bastionName,
		Children: []Node{
			{
				Name: cloudHypervisorName,
				Children: []Node{
					{Name: "host", OK: true},
					{Name: "assets", OK: false},
				},
			},
		},
	}

	var out bytes.Buffer
	if err := depTree.Render(&out); err != nil {
		t.Fatalf("render tree: %v", err)
	}

	got := out.String()
	for _, want := range []string{
		"bastion [x]",
		"└── cloud-hypervisor [x]",
		"    ├── host [ok]",
		"    └── assets [x]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered tree missing %q:\n%s", want, got)
		}
	}

	if depTree.Available() {
		t.Fatal("tree available = true, want false")
	}
}
