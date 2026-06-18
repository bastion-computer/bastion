package cli

import (
	"encoding/json"
	"errors"
	"io"
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
		newTemplatesExportCommand(opts),
		newTemplatesImportCommand(opts),
		newTemplatesRemoveCommand(opts),
	)

	return cmd
}

func newTemplatesCreateCommand(opts *rootOptions) *cobra.Command {
	var (
		key         string
		configValue string
		file        string
	)

	cmd := &cobra.Command{
		Use:   "create [--key KEY] (--config JSON | --file PATH)",
		Short: "Create an environment template",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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

			var templateKey *string
			if cmd.Flags().Changed("key") {
				templateKey = &key
			}

			template, err := apiClient(opts).CreateTemplate(cmd.Context(), template.CreateRequest{
				Key:    templateKey,
				Config: contents,
				Logs:   cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), template)
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "optional unique template key")
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

func newTemplatesExportCommand(opts *rootOptions) *cobra.Command {
	var (
		id  string
		key string
	)

	cmd := &cobra.Command{
		Use:   "export [--id ID | --key KEY]",
		Short: "Export a prepared template archive",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireIDOrKey(id, key); err != nil {
				return err
			}

			progress := newArchiveProgress(cmd.ErrOrStderr(), "exporting template", -1)
			err := apiClient(opts).ExportTemplate(cmd.Context(), id, key, io.MultiWriter(cmd.OutOrStdout(), progress))

			if finishErr := progress.finish(err == nil); finishErr != nil && err == nil {
				return finishErr
			}

			return err
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "template ID")
	cmd.Flags().StringVar(&key, "key", "", "template key")

	return cmd
}

func newTemplatesImportCommand(opts *rootOptions) *cobra.Command {
	var (
		key  string
		file string
	)

	cmd := &cobra.Command{
		Use:   "import [--key KEY] --file PATH",
		Short: "Import a prepared template archive",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if file == "" {
				return errors.New("specify --file")
			}

			archive, err := os.Open(file) //nolint:gosec // CLI user explicitly chooses the template archive path.
			if err != nil {
				return err
			}
			defer func() { _ = archive.Close() }()

			info, err := archive.Stat()
			if err != nil {
				return err
			}

			archiveSize := int64(-1)
			if info.Mode().IsRegular() {
				archiveSize = info.Size()
			}

			var templateKey *string
			if cmd.Flags().Changed("key") {
				templateKey = &key
			}

			progress := newArchiveProgress(cmd.ErrOrStderr(), "importing template", archiveSize)
			imported, err := apiClient(opts).ImportTemplate(cmd.Context(), template.ImportRequest{Key: templateKey, Archive: io.TeeReader(archive, progress), ArchiveSize: archiveSize})

			if finishErr := progress.finish(err == nil); finishErr != nil && err == nil {
				return finishErr
			}

			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), imported)
		},
	}
	cmd.Flags().StringVar(&key, "key", "", "optional unique template key")
	cmd.Flags().StringVar(&file, "file", "", "template archive file")

	return cmd
}
