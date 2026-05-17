// Package system checks and installs host dependencies needed by Bastion.
package system

import (
	"errors"
	"fmt"
	"io"
)

// ErrMissingDependencies reports an unavailable system dependency.
var ErrMissingDependencies = errors.New("error: missing dependencies")

// Node is one entry in a renderable system dependency tree.
type Node struct {
	Name     string
	OK       bool
	Children []Node
}

// Available reports whether this node and every child dependency is available.
func (n Node) Available() bool {
	if len(n.Children) == 0 {
		return n.OK
	}

	for _, child := range n.Children {
		if !child.Available() {
			return false
		}
	}

	return true
}

// Render writes the dependency tree rooted at n.
func (n Node) Render(w io.Writer) error {
	return renderNode(w, n, "", true, true)
}

func renderNode(w io.Writer, node Node, prefix string, last, root bool) error {
	linePrefix := prefix
	childPrefix := prefix

	if !root {
		connector := "├── "
		childPrefix += "│   "

		if last {
			connector = "└── "
			childPrefix = prefix + "    "
		}

		linePrefix += connector
	}

	if _, err := fmt.Fprintf(w, "%s%s [%s]\n", linePrefix, node.Name, nodeStatus(node)); err != nil {
		return err
	}

	for i, child := range node.Children {
		if err := renderNode(w, child, childPrefix, i == len(node.Children)-1, false); err != nil {
			return err
		}
	}

	return nil
}

func nodeStatus(node Node) string {
	if node.Available() {
		return "ok"
	}

	return "x"
}
