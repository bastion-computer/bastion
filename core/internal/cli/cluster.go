package cli

import (
	"errors"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/services/cluster"
)

const (
	clusterUse           = "cluster"
	clusterNodesUse      = "nodes"
	clusterNamespacesUse = "namespaces"
)

func newClusterCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   clusterUse,
		Short: "Manage a Bastion cluster",
	}
	cmd.AddCommand(
		newClusterNodesCommand(opts),
		newClusterNamespacesCommand(opts),
		newClusterUtilizationCommand(opts),
	)

	return cmd
}

func newClusterNodesCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   clusterNodesUse,
		Short: "Manage cluster nodes",
	}
	cmd.AddCommand(
		newClusterNodesCreateCommand(opts),
		newClusterNodesListCommand(opts),
		newClusterNodesGetCommand(opts),
		newClusterNodesRemoveCommand(opts),
	)

	return cmd
}

func newClusterNodesCreateCommand(opts *rootOptions) *cobra.Command {
	var (
		key    string
		apiURL string
	)

	cmd := &cobra.Command{
		Use:   "create [--key KEY] --url URL",
		Short: "Register a Bastion host API as a cluster node",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if apiURL == "" {
				return errors.New("specify --url")
			}

			var nodeKey *string
			if cmd.Flags().Changed("key") {
				nodeKey = &key
			}

			created, err := apiClient(opts).CreateClusterNode(cmd.Context(), cluster.CreateNodeRequest{Key: nodeKey, APIURL: apiURL})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), created)
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "optional unique node key")
	cmd.Flags().StringVar(&apiURL, "url", "", "host API URL for the node")

	return cmd
}

func newClusterNodesListCommand(opts *rootOptions) *cobra.Command {
	return newListCommand("List cluster nodes", func(cmd *cobra.Command, limit int, cursor string) (any, error) {
		return apiClient(opts).ListClusterNodes(cmd.Context(), limit, cursor)
	})
}

func newClusterNodesGetCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(getIDKeyUse, "Get a cluster node", "node ID", "node key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).GetClusterNode(cmd.Context(), id, key)
	})
}

func newClusterNodesRemoveCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(removeIDKeyUse, "Remove a cluster node", "node ID", "node key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).RemoveClusterNode(cmd.Context(), id, key)
	})
}

func newClusterNamespacesCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   clusterNamespacesUse,
		Short: "Manage cluster namespaces",
	}
	cmd.AddCommand(
		newClusterNamespacesCreateCommand(opts),
		newClusterNamespacesListCommand(opts),
		newClusterNamespacesGetCommand(opts),
		newClusterNamespacesRemoveCommand(opts),
	)

	return cmd
}

func newClusterNamespacesCreateCommand(opts *rootOptions) *cobra.Command {
	var (
		key    string
		vcpu   int64
		memory int64
		volume int64
	)

	cmd := &cobra.Command{
		Use:   "create [--key KEY] [--vcpu N] [--memory BYTES] [--volume BYTES]",
		Short: "Create a cluster namespace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var namespaceKey *string

			if cmd.Flags().Changed("key") {
				if strings.HasPrefix(key, "ns_") {
					return errors.New("namespace key cannot use reserved ns_ prefix")
				}

				namespaceKey = &key
			}

			created, err := apiClient(opts).CreateClusterNamespace(cmd.Context(), cluster.CreateNamespaceRequest{
				Key: namespaceKey,
				Limits: cluster.NamespaceLimits{
					VCPU:        vcpu,
					MemoryBytes: memory,
					VolumeBytes: volume,
				},
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), created)
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "optional unique namespace key")
	cmd.Flags().Int64Var(&vcpu, "vcpu", 0, "namespace vCPU limit")
	cmd.Flags().Int64Var(&memory, "memory", 0, "namespace memory limit in bytes")
	cmd.Flags().Int64Var(&volume, "volume", 0, "namespace volume limit in bytes")

	return cmd
}

func newClusterNamespacesListCommand(opts *rootOptions) *cobra.Command {
	return newListCommand("List cluster namespaces", func(cmd *cobra.Command, limit int, cursor string) (any, error) {
		return apiClient(opts).ListClusterNamespaces(cmd.Context(), limit, cursor)
	})
}

func newClusterNamespacesGetCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(getIDKeyUse, "Get a cluster namespace", "namespace ID", "namespace key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).GetClusterNamespace(cmd.Context(), id, key)
	})
}

func newClusterNamespacesRemoveCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(removeIDKeyUse, "Remove a cluster namespace", "namespace ID", "namespace key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).RemoveClusterNamespace(cmd.Context(), id, key)
	})
}

func newClusterUtilizationCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   utilizationUse,
		Short: "Show cluster utilization",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			utilization, err := apiClient(opts).GetClusterUtilization(cmd.Context())
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), utilization)
		},
	}
}
