// Package cluster contains cluster control plane API types.
package cluster

// Node describes one Bastion host API registered with the cluster.
type Node struct {
	ID        string  `json:"id"`
	Key       *string `json:"key,omitempty"`
	APIURL    string  `json:"apiUrl"`
	CreatedAt string  `json:"createdAt,omitempty"`
}

// CreateNodeRequest contains the fields needed to register a cluster node.
type CreateNodeRequest struct {
	Key    *string `json:"key,omitempty"`
	APIURL string  `json:"apiUrl"`
}

// NamespaceLimits describes optional namespace resource ceilings.
type NamespaceLimits struct {
	VCPU        int64 `json:"vcpu,omitempty"`
	MemoryBytes int64 `json:"memory,omitempty"`
	VolumeBytes int64 `json:"volume,omitempty"`
}

// Namespace describes one tenant namespace managed by the cluster.
type Namespace struct {
	ID        string          `json:"id"`
	Key       *string         `json:"key,omitempty"`
	Limits    NamespaceLimits `json:"limits"`
	CreatedAt string          `json:"createdAt,omitempty"`
}

// CreateNamespaceRequest contains the fields needed to create a namespace.
type CreateNamespaceRequest struct {
	Key    *string         `json:"key,omitempty"`
	Limits NamespaceLimits `json:"limits"`
}
