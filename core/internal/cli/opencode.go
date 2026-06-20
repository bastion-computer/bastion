package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

type openCodeRunner func(context.Context, io.Reader, io.Writer, io.Writer, string) error

func newOpenCodeCommand(opts *rootOptions) *cobra.Command {
	return newOpenCodeCommandWithRunner(opts, runOpenCodeAttach)
}

func newOpenCodeCommandWithRunner(opts *rootOptions, runner openCodeRunner) *cobra.Command {
	if runner == nil {
		runner = runOpenCodeAttach
	}

	var (
		id  string
		key string
	)

	cmd := &cobra.Command{
		Use:   "opencode [--id ID | --key KEY]",
		Short: "Attach OpenCode to an environment",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireIDOrKey(id, key); err != nil {
				return err
			}

			proxyURL := openCodeProxyURL(opts.apiURL, opts.namespace, id, key)

			return runner(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), proxyURL)
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "environment ID")
	cmd.Flags().StringVar(&key, "key", "", "environment key")

	return cmd
}

func runOpenCodeAttach(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, proxyURL string) error {
	executable, err := exec.LookPath("opencode")
	if err != nil {
		return errors.New("opencode is not available")
	}

	//nolint:gosec // The executable is resolved by the preflight check; arguments are fixed except the proxy URL.
	cmd := exec.CommandContext(ctx, executable, "attach", proxyURL)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("opencode attach failed: %w", err)
	}

	return nil
}

func openCodeProxyURL(apiURL, namespace, id, key string) string {
	baseURL := strings.TrimRight(apiURL, "/")
	prefix := "/v1"

	if namespace != "" {
		prefix += "/namespaces/" + url.PathEscape(namespace)
	}

	if key != "" {
		return baseURL + prefix + "/environments/by-key/" + url.PathEscape(key) + "/agents/opencode"
	}

	return baseURL + prefix + "/environments/" + url.PathEscape(id) + "/agents/opencode"
}
