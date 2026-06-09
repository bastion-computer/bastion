// Package cli builds the Bastion command-line interface.
package cli

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/client"
)

const (
	clientUse      = "client"
	environmentUse = "env"
	listUse        = "list"
	removeUse      = "remove"
	setUse         = "set"
	getIDKeyUse    = "get [--id ID | --key KEY]"
	removeIDKeyUse = "remove [--id ID | --key KEY]"
)

type (
	listCommandAction  func(*cobra.Command, int, string) (any, error)
	idKeyCommandAction func(*cobra.Command, string, string) (any, error)
)

func apiClient(opts *rootOptions) *client.Client {
	return client.New(opts.apiURL)
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")

	return encoder.Encode(value)
}

func newListCommand(short string, action listCommandAction) *cobra.Command {
	var (
		limit  int
		cursor string
	)

	cmd := &cobra.Command{
		Use:   listUse,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			value, err := action(cmd, limit, cursor)
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), value)
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum entries to return")
	cmd.Flags().StringVar(&cursor, "cursor", "", "pagination cursor")

	return cmd
}

func newIDKeyCommand(use, short, idUsage, keyUsage string, action idKeyCommandAction) *cobra.Command {
	var (
		id  string
		key string
	)

	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireIDOrKey(id, key); err != nil {
				return err
			}

			value, err := action(cmd, id, key)
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), value)
		},
	}
	cmd.Flags().StringVar(&id, "id", "", idUsage)
	cmd.Flags().StringVar(&key, "key", "", keyUsage)

	return cmd
}

func requireIDOrKey(id, key string) error {
	if (id == "") == (key == "") {
		return errors.New("specify exactly one of --id or --key")
	}

	return nil
}
