package cli

import (
	"errors"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/services/base"
)

func newBaseCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   baseUse,
		Short: "Manage the template base image",
	}
	cmd.AddCommand(
		newBaseBuildCommand(opts),
		newBaseGetCommand(opts),
		newBaseImportCommand(opts),
		newBaseExportCommand(opts),
	)

	return cmd
}

func newBaseBuildCommand(opts *rootOptions) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "build [--force]",
		Short: "Build the template base image",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			built, err := apiClient(opts).BuildBase(cmd.Context(), base.BuildRequest{Force: force, Logs: cmd.ErrOrStderr()})
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), built)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing base")

	return cmd
}

func newBaseGetCommand(opts *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Get the template base image",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			base, err := apiClient(opts).GetBase(cmd.Context())
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), base)
		},
	}
}

func newBaseImportCommand(opts *rootOptions) *cobra.Command {
	var (
		file  string
		force bool
	)

	cmd := &cobra.Command{
		Use:   "import --file PATH [--force]",
		Short: "Import a template base image archive",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if file == "" {
				return errors.New("specify --file")
			}

			archive, err := os.Open(file) //nolint:gosec // CLI user explicitly chooses the base archive path.
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

			progress := newArchiveProgress(cmd.ErrOrStderr(), "importing base", archiveSize)
			imported, err := apiClient(opts).ImportBase(cmd.Context(), base.ImportRequest{Force: force, Archive: io.TeeReader(archive, progress), ArchiveSize: archiveSize, Logs: cmd.ErrOrStderr()})

			if finishErr := progress.finish(err == nil); finishErr != nil && err == nil {
				return finishErr
			}

			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), imported)
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "base archive file")
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing base")

	return cmd
}

func newBaseExportCommand(opts *rootOptions) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export the template base image archive",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			progress := newArchiveProgress(cmd.ErrOrStderr(), "exporting base", -1)
			err := apiClient(opts).ExportBase(cmd.Context(), io.MultiWriter(cmd.OutOrStdout(), progress))

			if finishErr := progress.finish(err == nil); finishErr != nil && err == nil {
				return finishErr
			}

			return err
		},
	}

	return cmd
}
