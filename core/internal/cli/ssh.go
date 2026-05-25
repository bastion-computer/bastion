package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	"github.com/bastion-computer/bastion/core/internal/client"
	"github.com/bastion-computer/bastion/core/internal/sshtunnel"
)

type sshRunner func(context.Context, io.Reader, io.Writer, io.Writer, *client.Client, string, []string) error

func newSSHCommand(opts *rootOptions) *cobra.Command {
	return newSSHCommandWithRunner(opts, runAPISSH)
}

func newSSHCommandWithRunner(opts *rootOptions, runner sshRunner) *cobra.Command {
	if runner == nil {
		runner = runAPISSH
	}

	var (
		id  string
		key string
	)

	cmd := &cobra.Command{
		Use:   "ssh [--id ID | --key KEY] [-- COMMAND...]",
		Short: "Connect to an environment over SSH",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireIDOrKey(id, key); err != nil {
				return err
			}

			api := apiClient(opts)
			environmentID := id

			if key != "" {
				environment, err := api.GetEnvironmentByKey(cmd.Context(), key)
				if err != nil {
					return err
				}

				if environment.ID == "" {
					return errors.New("environment key lookup returned empty id")
				}

				environmentID = environment.ID
			}

			return runner(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), api, environmentID, args)
		},
	}
	cmd.Flags().StringVar(&id, "id", "", "environment ID")
	cmd.Flags().StringVar(&key, "key", "", "environment key")

	return cmd
}

func runAPISSH(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, api *client.Client, environmentID string, command []string) error {
	req := sshtunnel.Request{Command: command}

	interactive := len(command) == 0 && isTerminal(stdin) && isTerminal(stdout)
	if interactive {
		req.PTY = true
		req.Term = terminalName()
		req.Width, req.Height = terminalSize(stdout)
	}

	stream, err := api.OpenSSH(ctx, environmentID, req)
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()

	if interactive {
		restore, err := makeRawTerminal(stdin)
		if err != nil {
			return err
		}
		defer func() { _ = restore() }()
	}

	writer := sshtunnel.NewFrameWriter(stream)
	if interactive {
		stopResizeForwarding := forwardWindowChanges(ctx, writer, stdout)
		defer stopResizeForwarding()
	}

	go copySSHInput(writer, stdin)

	return readSSHOutput(stream, stdout, stderr)
}

func copySSHInput(writer *sshtunnel.FrameWriter, stdin io.Reader) {
	buf := make([]byte, 32*1024)
	for {
		n, err := stdin.Read(buf)
		if n > 0 {
			if writeErr := writer.WriteFrame(sshtunnel.FrameStdin, buf[:n]); writeErr != nil {
				return
			}
		}

		if err != nil {
			if err == io.EOF {
				_ = writer.WriteFrame(sshtunnel.FrameStdinEOF, nil)
			}

			return
		}
	}
}

func readSSHOutput(stream io.Reader, stdout, stderr io.Writer) error {
	for {
		frameType, payload, err := sshtunnel.ReadFrame(stream)
		if err != nil {
			return fmt.Errorf("host API SSH stream ended before exit status: %w", err)
		}

		switch frameType {
		case sshtunnel.FrameStdout:
			if _, err := stdout.Write(payload); err != nil {
				return err
			}
		case sshtunnel.FrameStderr:
			if _, err := stderr.Write(payload); err != nil {
				return err
			}
		case sshtunnel.FrameExit:
			var status sshtunnel.ExitStatus
			if err := json.Unmarshal(payload, &status); err != nil {
				return fmt.Errorf("decode SSH exit status: %w", err)
			}

			if status.Code != 0 {
				return sshExitError{code: status.Code}
			}

			return nil
		case sshtunnel.FrameError:
			return fmt.Errorf("host API SSH error: %s", string(payload))
		}
	}
}

type sshExitError struct {
	code int
}

func (e sshExitError) Error() string {
	return fmt.Sprintf("remote command exited with status %d", e.code)
}

type fileDescriptor interface {
	Fd() uintptr
}

func isTerminal(value any) bool {
	fd, ok := descriptor(value)
	if !ok {
		return false
	}

	_, err := unix.IoctlGetTermios(fd, unix.TCGETS)

	return err == nil
}

func descriptor(value any) (int, bool) {
	file, ok := value.(fileDescriptor)
	if !ok {
		return 0, false
	}

	return int(file.Fd()), true
}

func terminalName() string {
	if term := os.Getenv("TERM"); term != "" {
		return term
	}

	return "xterm"
}

func terminalSize(value any) (int, int) {
	fd, ok := descriptor(value)
	if !ok {
		return 80, 24
	}

	size, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil || size.Col == 0 || size.Row == 0 {
		return 80, 24
	}

	return int(size.Col), int(size.Row)
}

func makeRawTerminal(value any) (func() error, error) {
	fd, ok := descriptor(value)
	if !ok {
		return nil, errors.New("stdin is not a terminal")
	}

	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return nil, fmt.Errorf("get terminal state: %w", err)
	}

	original := *termios
	raw := original
	raw.Iflag &^= unix.BRKINT | unix.ICRNL | unix.INPCK | unix.ISTRIP | unix.IXON
	raw.Oflag &^= unix.OPOST
	raw.Cflag |= unix.CS8
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.IEXTEN | unix.ISIG
	raw.Cc[unix.VMIN] = 1
	raw.Cc[unix.VTIME] = 0

	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &raw); err != nil {
		return nil, fmt.Errorf("set terminal raw mode: %w", err)
	}

	return func() error {
		return unix.IoctlSetTermios(fd, unix.TCSETS, &original)
	}, nil
}

func forwardWindowChanges(ctx context.Context, writer *sshtunnel.FrameWriter, stdout io.Writer) func() {
	signals := make(chan os.Signal, 1)
	done := make(chan struct{})

	signal.Notify(signals, syscall.SIGWINCH)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-signals:
				sendWindowSize(writer, stdout)
			}
		}
	}()

	return func() {
		signal.Stop(signals)
		close(done)
	}
}

func sendWindowSize(writer *sshtunnel.FrameWriter, stdout io.Writer) {
	width, height := terminalSize(stdout)

	payload, err := json.Marshal(sshtunnel.Resize{Width: width, Height: height})
	if err != nil {
		return
	}

	_ = writer.WriteFrame(sshtunnel.FrameResize, payload)
}
