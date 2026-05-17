// Package system manages host-level dependencies used by Bastion.
package system

import "github.com/bastion-computer/bastion/core/internal/system/dependencies"

// Node is a system dependency tree node.
type Node = dependencies.Node

// AddOptions configures dependency setup.
type AddOptions = dependencies.AddOptions

// AddResult describes dependency setup output.
type AddResult = dependencies.AddResult

// RemoveResult describes dependency removal output.
type RemoveResult = dependencies.RemoveResult
