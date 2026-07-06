package cli

import (
	"errors"

	"github.com/spf13/cobra"

	clusterservice "github.com/bastion-computer/bastion/core/internal/services/cluster"
)

const (
	clusterNodesUse      = "nodes"
	clusterNamespacesUse = "namespaces"
)

func newClusterCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   clusterUse,
		Short: "Manage cluster control plane resources",
	}
	cmd.AddCommand(
		newClusterNodesCommand(opts),
		newClusterNamespacesCommand(opts),
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
		key     string
		nodeURL string
	)

	cmd := &cobra.Command{
		Use:   "create [--key KEY] --url URL",
		Short: "Add a Bastion API node to the cluster",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !cmd.Flags().Changed("url") || nodeURL == "" {
				return errors.New("specify --url")
			}

			var nodeKey *string
			if cmd.Flags().Changed("key") {
				nodeKey = &key
			}

			created, err := apiClient(opts).CreateClusterNode(cmd.Context(), clusterservice.CreateNodeRequest{Key: nodeKey, URL: nodeURL, Logs: cmd.ErrOrStderr()})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), created)
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "optional unique node key")
	cmd.Flags().StringVar(&nodeURL, "url", "", "Bastion API URL for the node")

	return cmd
}

func newClusterNodesListCommand(opts *rootOptions) *cobra.Command {
	return newListCommand("List cluster nodes", func(cmd *cobra.Command, limit int, cursor string) (any, error) {
		return apiClient(opts).ListClusterNodes(cmd.Context(), limit, cursor)
	})
}

func newClusterNodesGetCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(getIDKeyUse, "Get a cluster node", "cluster node ID", "cluster node key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).GetClusterNode(cmd.Context(), id, key)
	})
}

func newClusterNodesRemoveCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(removeIDKeyUse, "Remove a cluster node", "cluster node ID", "cluster node key", func(cmd *cobra.Command, id, key string) (any, error) {
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
	var key string

	cmd := &cobra.Command{
		Use:   "create [--key KEY]",
		Short: "Create a cluster namespace",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var namespaceKey *string
			if cmd.Flags().Changed("key") {
				namespaceKey = &key
			}

			created, err := apiClient(opts).CreateClusterNamespace(cmd.Context(), clusterservice.CreateNamespaceRequest{Key: namespaceKey})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), created)
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "optional unique namespace key")

	return cmd
}

func newClusterNamespacesListCommand(opts *rootOptions) *cobra.Command {
	return newListCommand("List cluster namespaces", func(cmd *cobra.Command, limit int, cursor string) (any, error) {
		return apiClient(opts).ListClusterNamespaces(cmd.Context(), limit, cursor)
	})
}

func newClusterNamespacesGetCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(getIDKeyUse, "Get a cluster namespace", "cluster namespace ID", "cluster namespace key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).GetClusterNamespace(cmd.Context(), id, key)
	})
}

func newClusterNamespacesRemoveCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(removeIDKeyUse, "Remove a cluster namespace", "cluster namespace ID", "cluster namespace key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).RemoveClusterNamespace(cmd.Context(), id, key)
	})
}
