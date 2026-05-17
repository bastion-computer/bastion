// Package utilities checks and installs host utilities.
package utilities

import "github.com/bastion-computer/bastion/core/internal/system/dependencies"

// Utility describes a required host utility.
type Utility struct {
	Name string
}

// Registry checks required host utilities.
type Registry struct {
	Required []Utility
	LookPath func(string) (string, error)
}

// Missing returns required utilities that are not available on PATH.
func (r Registry) Missing() []Utility {
	missing := make([]Utility, 0)

	for _, utility := range r.Required {
		if _, err := r.LookPath(utility.Name); err != nil {
			missing = append(missing, utility)
		}
	}

	return missing
}

// Node returns a dependency tree node for required utilities.
func (r Registry) Node() dependencies.Node {
	children := make([]dependencies.Node, 0, len(r.Required))

	for _, utility := range r.Required {
		_, err := r.LookPath(utility.Name)
		children = append(children, dependencies.Node{Name: utility.Name, OK: err == nil})
	}

	return dependencies.Node{Name: "utilities", Children: children}
}

func names(values []Utility) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.Name)
	}

	return out
}
