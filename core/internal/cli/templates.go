package cli

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/services/template"
)

func newTemplatesCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "templates",
		Short: "Manage environment templates",
	}
	cmd.AddCommand(
		newTemplatesCreateCommand(opts),
		newTemplatesListCommand(opts),
		newTemplatesGetCommand(opts),
		newTemplatesRemoveCommand(opts),
	)

	return cmd
}

func newTemplatesCreateCommand(opts *rootOptions) *cobra.Command {
	var (
		configValue string
		file        string
	)

	cmd := &cobra.Command{
		Use:   "create KEY (--config JSON | --file PATH)",
		Short: "Create an environment template",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if (configValue == "") == (file == "") {
				return errors.New("specify exactly one of --config or --file")
			}

			contents := json.RawMessage(configValue)

			if file != "" {
				fileContents, err := os.ReadFile(file) //nolint:gosec // CLI user explicitly chooses the template file path.
				if err != nil {
					return err
				}

				contents = json.RawMessage(fileContents)
			}

			template, err := apiClient(opts).CreateTemplate(cmd.Context(), template.CreateRequest{
				Key:    args[0],
				Config: contents,
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), template)
		},
	}
	cmd.Flags().StringVar(&configValue, "config", "", "inline template JSON")
	cmd.Flags().StringVar(&file, "file", "", "template JSON file")

	return cmd
}

func newTemplatesListCommand(opts *rootOptions) *cobra.Command {
	return newListCommand("List templates", func(cmd *cobra.Command, limit int, cursor string) (any, error) {
		return apiClient(opts).ListTemplates(cmd.Context(), limit, cursor)
	})
}

func newTemplatesGetCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(getIDKeyUse, "Get a template", "template ID", "template key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).GetTemplate(cmd.Context(), id, key)
	})
}

func newTemplatesRemoveCommand(opts *rootOptions) *cobra.Command {
	return newIDKeyCommand(removeIDKeyUse, "Remove a template", "template ID", "template key", func(cmd *cobra.Command, id, key string) (any, error) {
		return apiClient(opts).RemoveTemplate(cmd.Context(), id, key)
	})
}
