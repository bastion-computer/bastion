package cli

import (
	"errors"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/services/environment"
)

func newEnvironmentCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   environmentUse,
		Short: "Manage environments",
	}
	cmd.AddCommand(
		newEnvironmentCreateCommand(opts),
		newEnvironmentListCommand(opts),
		newEnvironmentGetCommand(opts),
		newEnvironmentTunnelsCommand(opts),
		newEnvironmentRemoveCommand(opts),
	)

	return cmd
}

func newEnvironmentCreateCommand(opts *rootOptions) *cobra.Command {
	var (
		key         string
		templateID  string
		templateKey string
		tags        []string
	)

	cmd := &cobra.Command{
		Use:   "create (--template-id ID | --template-key KEY) [--key KEY]",
		Short: "Create an environment from a template",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if (templateID == "") == (templateKey == "") {
				return errors.New("specify exactly one of --template-id or --template-key")
			}

			var environmentKey *string
			if cmd.Flags().Changed("key") {
				environmentKey = &key
			}

			created, err := apiClient(opts).CreateEnvironment(cmd.Context(), environment.CreateRequest{
				Key:         environmentKey,
				TemplateID:  templateID,
				TemplateKey: templateKey,
				Tags:        tags,
				Logs:        cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), created)
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "optional unique environment key")
	cmd.Flags().StringVar(&templateID, "template-id", "", "template ID")
	cmd.Flags().StringVar(&templateKey, "template-key", "", "template key")
	cmd.Flags().StringArrayVarP(&tags, "tag", "t", nil, "environment tag (repeatable)")

	return cmd
}

func newEnvironmentListCommand(opts *rootOptions) *cobra.Command {
	var (
		limit  int
		cursor string
		tags   []string
	)

	cmd := &cobra.Command{
		Use:   listUse,
		Short: "List environments",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			value, err := apiClient(opts).ListEnvironments(cmd.Context(), limit, cursor, tags)
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), value)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum entries to return")
	cmd.Flags().StringVar(&cursor, "cursor", "", "pagination cursor")
	cmd.Flags().StringArrayVarP(&tags, "tag", "t", nil, "environment tag filter (repeatable)")

	return cmd
}

func newEnvironmentGetCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(getIDKeyUse, "Get an environment", "environment ID", "environment key", func(cmd *cobra.Command, id, key string) (any, error) {
		if key != "" {
			return apiClient(opts).GetEnvironmentByKey(cmd.Context(), key)
		}

		return apiClient(opts).GetEnvironment(cmd.Context(), id)
	})
}

func newEnvironmentTunnelsCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand("tunnels [--id ID | --key KEY]", "List environment tunnel URLs", "environment ID", "environment key", func(cmd *cobra.Command, id, key string) (any, error) {
		tunnels, err := apiClient(opts).GetEnvironmentTunnels(cmd.Context(), id, key)
		if err != nil {
			return nil, err
		}

		for i := range tunnels.Entries {
			tunnels.Entries[i].URL = environmentTunnelURL(opts.apiURL, id, key, tunnels.Entries[i].Name, opts.namespaceID, opts.namespaceKey)
		}

		return tunnels, nil
	})
}

func environmentTunnelURL(apiURL, id, key, name, namespaceID, namespaceKey string) string {
	baseURL := strings.TrimRight(apiURL, "/")

	var value string

	if key != "" {
		value = baseURL + "/v1/environments/by-key/" + url.PathEscape(key) + "/tunnels/" + url.PathEscape(name)
	} else {
		value = baseURL + "/v1/environments/" + url.PathEscape(id) + "/tunnels/" + url.PathEscape(name)
	}

	return urlWithNamespace(value, namespaceID, namespaceKey)
}

func urlWithNamespace(value, namespaceID, namespaceKey string) string {
	if namespaceID == "" && namespaceKey == "" {
		return value
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}

	namespacedPath, ok := namespaceResourcePath(parsed.EscapedPath(), namespaceID, namespaceKey)
	if !ok {
		return value
	}

	decodedPath, err := url.PathUnescape(namespacedPath)
	if err != nil {
		return value
	}

	parsed.Path = decodedPath
	parsed.RawPath = namespacedPath

	return parsed.String()
}

func namespaceResourcePath(path, namespaceID, namespaceKey string) (string, bool) {
	var namespacePath string

	switch {
	case namespaceID != "":
		namespacePath = "/namespaces/" + url.PathEscape(namespaceID)
	case namespaceKey != "":
		namespacePath = "/namespaces/by-key/" + url.PathEscape(namespaceKey)
	default:
		return path, false
	}

	for _, resource := range []string{"/secrets", "/templates", "/environments"} {
		marker := "/v1" + resource
		searchFrom := 0

		for {
			index := strings.Index(path[searchFrom:], marker)
			if index < 0 {
				break
			}

			index += searchFrom
			end := index + len(marker)

			if end == len(path) || path[end] == '/' {
				return path[:index] + "/v1" + namespacePath + path[index+len("/v1"):], true
			}

			searchFrom = end
		}
	}

	return path, false
}

func newEnvironmentRemoveCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(removeIDKeyUse, "Remove an environment", "environment ID", "environment key", func(cmd *cobra.Command, id, key string) (any, error) {
		if key != "" {
			return apiClient(opts).RemoveEnvironmentByKey(cmd.Context(), key)
		}

		return apiClient(opts).RemoveEnvironment(cmd.Context(), id)
	})
}
