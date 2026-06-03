package cli

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bastion-computer/bastion/core/internal/client"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
)

const (
	muxSessionName  = "bastion"
	muxEnvPageLimit = 100
)

//go:embed bastion-tmux.conf
var bastionTmuxConfig []byte

type tmuxRunner interface {
	run(context.Context, ...string) (string, error)
}

type osTmuxRunner struct{}

type muxTarget struct {
	session string
	window  string
	pane    string
}

func newMuxCommand(opts *rootOptions) *cobra.Command {
	return newMuxCommandWithRunner(opts, osTmuxRunner{})
}

func newMuxCommandWithRunner(opts *rootOptions, tmux tmuxRunner) *cobra.Command {
	if tmux == nil {
		tmux = osTmuxRunner{}
	}

	cmd := &cobra.Command{
		Use:   "mux",
		Short: "Open a tmux session for Bastion environments",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runMux(cmd, opts, tmux)
		},
	}
	cmd.AddCommand(
		newMuxPendingCommand(opts, tmux),
		newMuxSelectCommand(opts, tmux),
		newMuxConnectCommand(tmux),
	)

	return cmd
}

func newMuxPendingCommand(opts *rootOptions, tmux tmuxRunner) *cobra.Command {
	return &cobra.Command{
		Use:    "pending",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			target, err := currentMuxTarget(cmd.Context(), tmux)
			if err != nil {
				return err
			}

			if err := waitForMuxClient(cmd.Context(), tmux, target.session); err != nil {
				return err
			}

			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Select a Bastion environment from the menu to start SSH.")
			if err := runMuxSelect(cmd.Context(), tmux, apiClient(opts), target); err != nil {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), err)
			}

			<-cmd.Context().Done()

			return nil
		},
	}
}

func newMuxSelectCommand(opts *rootOptions, tmux tmuxRunner) *cobra.Command {
	var target muxTarget

	cmd := &cobra.Command{
		Use:    "select --target-window WINDOW --target-pane PANE",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireMuxTarget(target); err != nil {
				return err
			}

			return runMuxSelect(cmd.Context(), tmux, apiClient(opts), target)
		},
	}
	cmd.Flags().StringVar(&target.session, "target-session", muxSessionName, "tmux session to inspect")
	cmd.Flags().StringVar(&target.window, "target-window", "", "tmux window to rename")
	cmd.Flags().StringVar(&target.pane, "target-pane", "", "tmux pane to replace")

	return cmd
}

func newMuxConnectCommand(tmux tmuxRunner) *cobra.Command {
	var (
		target        muxTarget
		environmentID string
		name          string
	)

	cmd := &cobra.Command{
		Use:    "connect --target-window WINDOW --target-pane PANE --id ID --name NAME",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := requireMuxTarget(target); err != nil {
				return err
			}

			if environmentID == "" || name == "" {
				return errors.New("environment id and name are required")
			}

			return runMuxConnect(cmd.Context(), tmux, target, environmentID, name)
		},
	}
	cmd.Flags().StringVar(&target.session, "target-session", muxSessionName, "tmux session to inspect")
	cmd.Flags().StringVar(&target.window, "target-window", "", "tmux window to rename")
	cmd.Flags().StringVar(&target.pane, "target-pane", "", "tmux pane to replace")
	cmd.Flags().StringVar(&environmentID, "id", "", "environment ID")
	cmd.Flags().StringVar(&name, "name", "", "window base name")

	return cmd
}

func runMux(cmd *cobra.Command, opts *rootOptions, tmux tmuxRunner) error {
	if os.Getenv("TMUX") == "" && (!isTerminal(cmd.InOrStdin()) || !isTerminal(cmd.OutOrStdout())) {
		return errors.New("bastion mux requires an interactive terminal")
	}

	if _, err := exec.LookPath("tmux"); err != nil {
		return errors.New("tmux is not available")
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve bastion executable: %w", err)
	}

	_, _, err = ensureMuxSession(cmd.Context(), tmux, executable, opts.apiURL)
	if err != nil {
		return err
	}

	if os.Getenv("TMUX") != "" {
		if _, err := tmux.run(cmd.Context(), "switch-client", "-t", muxSessionName); err != nil {
			return err
		}

		return nil
	}

	_, err = tmux.run(cmd.Context(), "attach-session", "-t", muxSessionName)

	return err
}

func ensureMuxSession(ctx context.Context, tmux tmuxRunner, executable, apiURL string) (bool, muxTarget, error) {
	if tmuxHasSession(ctx, tmux) {
		return false, muxTarget{session: muxSessionName}, configureMuxSession(ctx, tmux, executable, apiURL)
	}

	target, err := createMuxSession(ctx, tmux, executable, apiURL)
	if err != nil {
		return false, muxTarget{}, err
	}

	if err := configureMuxSession(ctx, tmux, executable, apiURL); err != nil {
		return false, muxTarget{}, err
	}

	return true, target, nil
}

func tmuxHasSession(ctx context.Context, tmux tmuxRunner) bool {
	_, err := tmux.run(ctx, "has-session", "-t", muxSessionName)

	return err == nil
}

func createMuxSession(ctx context.Context, tmux tmuxRunner, executable, apiURL string) (muxTarget, error) {
	output, err := tmux.run(ctx, "new-session", "-d", "-P", "-F", "#{window_id}\t#{pane_id}", "-s", muxSessionName, "-n", "select", muxPendingShellCommand(executable, apiURL))
	if err != nil {
		return muxTarget{}, err
	}

	fields := strings.Split(strings.TrimSpace(output), "\t")
	if len(fields) != 2 || fields[0] == "" || fields[1] == "" {
		return muxTarget{}, fmt.Errorf("tmux new-session returned unexpected target %q", strings.TrimSpace(output))
	}

	return muxTarget{session: muxSessionName, window: fields[0], pane: fields[1]}, nil
}

func configureMuxSession(ctx context.Context, tmux tmuxRunner, executable, apiURL string) error {
	commands := [][]string{
		{"set-environment", "-t", muxSessionName, "BASTION_API_URL", apiURL},
		{"set-option", "-q", "-t", muxSessionName, "@bastion_mux_pending_command", muxPendingShellCommand(executable, apiURL)},
	}

	for _, args := range commands {
		if _, err := tmux.run(ctx, args...); err != nil {
			return err
		}
	}

	config, err := os.CreateTemp("", "bastion-tmux-*.conf")
	if err != nil {
		return fmt.Errorf("create tmux config: %w", err)
	}
	defer func() { _ = os.Remove(config.Name()) }()

	if _, err := config.Write(bastionTmuxConfig); err != nil {
		_ = config.Close()

		return fmt.Errorf("write tmux config: %w", err)
	}

	if err := config.Close(); err != nil {
		return fmt.Errorf("close tmux config: %w", err)
	}

	_, err = tmux.run(ctx, "source-file", config.Name())

	return err
}

func currentMuxTarget(ctx context.Context, tmux tmuxRunner) (muxTarget, error) {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return muxTarget{}, errors.New("TMUX_PANE is not set")
	}

	output, err := tmux.run(ctx, "display-message", "-p", "-t", pane, "#{session_name}\t#{window_id}\t#{pane_id}")
	if err != nil {
		return muxTarget{}, err
	}

	fields := strings.Split(strings.TrimSpace(output), "\t")
	if len(fields) != 3 || fields[0] == "" || fields[1] == "" || fields[2] == "" {
		return muxTarget{}, fmt.Errorf("tmux display-message returned unexpected target %q", strings.TrimSpace(output))
	}

	return muxTarget{session: fields[0], window: fields[1], pane: fields[2]}, nil
}

func waitForMuxClient(ctx context.Context, tmux tmuxRunner, session string) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		clients, err := tmux.run(ctx, "list-clients", "-t", session, "-F", "#{client_name}")
		if err == nil && strings.TrimSpace(clients) != "" {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func runMuxSelect(ctx context.Context, tmux tmuxRunner, api *client.Client, target muxTarget) error {
	environments, err := collectMuxEnvironments(ctx, api)
	if err != nil {
		return err
	}

	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve bastion executable: %w", err)
	}

	_, err = tmux.run(ctx, muxMenuArgs(executable, target, environments)...)

	return err
}

func runMuxConnect(ctx context.Context, tmux tmuxRunner, target muxTarget, environmentID, baseName string) error {
	windowList, err := tmux.run(ctx, "list-windows", "-t", target.session, "-F", "#{window_id}\t#{@bastion_environment_id}")
	if err != nil {
		return err
	}

	name := muxWindowName(baseName, muxSameEnvironmentCount(windowList, environmentID, target.window))
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve bastion executable: %w", err)
	}

	commands := [][]string{
		{"set-window-option", "-q", "-t", target.window, "@bastion_environment_id", environmentID},
		{"rename-window", "-t", target.window, name},
		{"respawn-pane", "-k", "-t", target.pane, muxSSHShellCommand(executable, environmentID)},
	}

	for _, args := range commands {
		if _, err := tmux.run(ctx, args...); err != nil {
			return err
		}
	}

	return nil
}

func muxMenuArgs(executable string, target muxTarget, environments []environment.Environment) []string {
	args := []string{
		"display-menu",
		"-t",
		target.pane,
		"-x",
		"C",
		"-y",
		"C",
		"-T",
		"Bastion environments",
	}

	if len(environments) == 0 {
		return append(args, "No environments found", "", "")
	}

	for _, env := range environments {
		args = append(args,
			muxEnvironmentMenuLabel(env),
			"",
			muxConnectTmuxCommand(executable, target, env.ID, muxEnvironmentLabel(env)),
		)
	}

	return args
}

func muxConnectTmuxCommand(executable string, target muxTarget, environmentID, name string) string {
	return "run-shell -b " + tmuxQuote(muxConnectTargetShellCommand(executable, target, environmentID, name))
}

func requireMuxTarget(target muxTarget) error {
	if target.session == "" || target.window == "" || target.pane == "" {
		return errors.New("target session, window, and pane are required")
	}

	return nil
}

func collectMuxEnvironments(ctx context.Context, api *client.Client) ([]environment.Environment, error) {
	var (
		cursor       string
		environments []environment.Environment
	)

	for {
		page, err := api.ListEnvironments(ctx, muxEnvPageLimit, cursor, nil)
		if err != nil {
			return nil, err
		}

		environments = append(environments, page.Entries...)
		if page.Cursor == nil || *page.Cursor == "" {
			return environments, nil
		}

		cursor = *page.Cursor
	}
}

func muxEnvironmentLabel(env environment.Environment) string {
	if env.Key != nil && *env.Key != "" {
		return *env.Key
	}

	return env.ID
}

func muxEnvironmentMenuLabel(env environment.Environment) string {
	label := muxEnvironmentLabel(env)
	if env.Key != nil && *env.Key != "" {
		return label + "  [" + env.ID + "]  " + env.Status
	}

	return label + "  " + env.Status
}

func muxWindowName(base string, sameEnvironmentCount int) string {
	if sameEnvironmentCount == 0 {
		return base
	}

	return fmt.Sprintf("%s (%d)", base, sameEnvironmentCount+1)
}

func muxSameEnvironmentCount(windowList, environmentID, targetWindow string) int {
	var count int

	for _, line := range strings.Split(strings.TrimSpace(windowList), "\n") {
		if line == "" {
			continue
		}

		fields := strings.SplitN(line, "\t", 2)
		if len(fields) != 2 || fields[0] == targetWindow {
			continue
		}

		if fields[1] == environmentID {
			count++
		}
	}

	return count
}

func muxPendingShellCommand(executable, apiURL string) string {
	return "BASTION_API_URL=" + shellQuote(apiURL) + " " + shellCommand(executable, "mux", "pending")
}

func muxConnectTargetShellCommand(executable string, target muxTarget, environmentID, name string) string {
	return shellCommand(executable, "mux", "connect", "--target-session", target.session, "--target-window", target.window, "--target-pane", target.pane, "--id", environmentID, "--name", name)
}

func muxSSHShellCommand(executable, environmentID string) string {
	return shellCommand(executable, "ssh", "--id", environmentID)
}

func shellCommand(executable string, args ...string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, shellQuote(executable))
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}

	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}

	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func tmuxQuote(value string) string {
	if value == "" {
		return "''"
	}

	if !strings.ContainsAny(value, " \t\n'\"\\;") {
		return value
	}

	return "\"" + strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "\n", "\\n").Replace(value) + "\""
}

func (osTmuxRunner) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "tmux", args...)
	if tmuxCommandNeedsTerminal(args) {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
		}

		return "", nil
	}

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(output.String())
		if message == "" {
			return "", fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
		}

		return "", fmt.Errorf("tmux %s: %w: %s", strings.Join(args, " "), err, message)
	}

	return output.String(), nil
}

func tmuxCommandNeedsTerminal(args []string) bool {
	return len(args) > 0 && args[0] == "attach-session"
}
