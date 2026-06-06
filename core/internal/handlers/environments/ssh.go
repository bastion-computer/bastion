package environments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/ssh"

	"github.com/bastion-computer/bastion/core/internal/handlers"
	"github.com/bastion-computer/bastion/core/internal/services/environment"
	"github.com/bastion-computer/bastion/core/pkg/sshtunnel"
)

const sshDialTimeout = 15 * time.Second

// SSH handles API-managed SSH sessions.
func (h Handler) SSH(c *gin.Context) {
	var req sshtunnel.Request
	if !handlers.BindJSON(c, &req) {
		return
	}

	connection, err := h.environments.SSHConnection(c.Request.Context(), c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		c.JSON(handlers.ErrorStatus(err), gin.H{"error": err.Error()})

		return
	}

	stream, err := hijackSSH(c.Writer)
	if err != nil {
		_ = c.Error(err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})

		return
	}
	defer func() { _ = stream.Close() }()

	if err := h.sshRunner(c.Request.Context(), stream, connection, req); err != nil {
		_ = sshtunnel.WriteFrame(stream, sshtunnel.FrameError, []byte(err.Error()))
	}
}

func hijackSSH(w http.ResponseWriter) (io.ReadWriteCloser, error) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("host API server does not support connection hijacking")
	}

	conn, rw, err := hijacker.Hijack()
	if err != nil {
		return nil, fmt.Errorf("hijack SSH connection: %w", err)
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("clear SSH connection deadline: %w", err)
	}

	if _, err := rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: " + sshtunnel.Protocol + "\r\n\r\n"); err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("write SSH upgrade response: %w", err)
	}

	if err := rw.Flush(); err != nil {
		_ = conn.Close()

		return nil, fmt.Errorf("flush SSH upgrade response: %w", err)
	}

	return conn, nil
}

func runSSHSession(ctx context.Context, stream io.ReadWriteCloser, connection environment.SSHConnection, req sshtunnel.Request) error {
	client, err := dialSSH(ctx, connection)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	return proxySSHSession(ctx, stream, client, req)
}

func dialSSH(ctx context.Context, connection environment.SSHConnection) (*ssh.Client, error) {
	key, err := os.ReadFile(connection.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("read SSH private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse SSH private key: %w", err)
	}

	addr := net.JoinHostPort(connection.Host, strconv.Itoa(connection.Port))
	dialer := net.Dialer{Timeout: sshDialTimeout}

	tcpConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dial environment SSH: %w", err)
	}

	config := &ssh.ClientConfig{
		User:            connection.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // Guest keys are ephemeral Bastion VM keys.
		Timeout:         sshDialTimeout,
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(tcpConn, addr, config)
	if err != nil {
		_ = tcpConn.Close()

		return nil, fmt.Errorf("start environment SSH client: %w", err)
	}

	return ssh.NewClient(sshConn, chans, reqs), nil
}

func proxySSHSession(ctx context.Context, stream io.ReadWriteCloser, client *ssh.Client, req sshtunnel.Request) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("create SSH session: %w", err)
	}
	defer func() { _ = session.Close() }()

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("open SSH stdin: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open SSH stdout: %w", err)
	}

	stderr, err := session.StderrPipe()
	if err != nil {
		return fmt.Errorf("open SSH stderr: %w", err)
	}

	if req.PTY {
		if err := requestPty(session, req); err != nil {
			return err
		}
	}

	writer := sshtunnel.NewFrameWriter(stream)
	copyErrs := make(chan error, 2)

	var wg sync.WaitGroup

	wg.Add(2)
	go copySSHOutput(&wg, copyErrs, writer, sshtunnel.FrameStdout, stdout)
	go copySSHOutput(&wg, copyErrs, writer, sshtunnel.FrameStderr, stderr)

	go readSSHInput(stream, session, stdin)

	if err := startSSHSession(session, req.Command); err != nil {
		return err
	}

	waitErr := waitSSHSession(ctx, session, client)

	wg.Wait()
	close(copyErrs)

	for copyErr := range copyErrs {
		if copyErr != nil {
			return copyErr
		}
	}

	return writeSSHExit(writer, waitErr)
}

func requestPty(session *ssh.Session, req sshtunnel.Request) error {
	term := req.Term
	if term == "" {
		term = "xterm"
	}

	width := req.Width
	if width <= 0 {
		width = 80
	}

	height := req.Height
	if height <= 0 {
		height = 24
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty(term, height, width, modes); err != nil {
		return fmt.Errorf("request SSH pty: %w", err)
	}

	return nil
}

func startSSHSession(session *ssh.Session, command []string) error {
	if len(command) == 0 {
		if err := session.Shell(); err != nil {
			return fmt.Errorf("start SSH shell: %w", err)
		}

		return nil
	}

	if err := session.Start(strings.Join(command, " ")); err != nil {
		return fmt.Errorf("start SSH command: %w", err)
	}

	return nil
}

func copySSHOutput(wg *sync.WaitGroup, errCh chan<- error, writer *sshtunnel.FrameWriter, frameType byte, src io.Reader) {
	defer wg.Done()

	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if writeErr := writer.WriteFrame(frameType, buf[:n]); writeErr != nil {
				errCh <- writeErr

				return
			}
		}

		if err != nil {
			if !errors.Is(err, io.EOF) {
				errCh <- err
			}

			return
		}
	}
}

func readSSHInput(stream io.Reader, session *ssh.Session, stdin io.WriteCloser) {
	defer func() { _ = stdin.Close() }()

	for {
		frameType, payload, err := sshtunnel.ReadFrame(stream)
		if err != nil {
			_ = session.Close()

			return
		}

		switch frameType {
		case sshtunnel.FrameStdin:
			if _, err := stdin.Write(payload); err != nil {
				_ = session.Close()

				return
			}
		case sshtunnel.FrameStdinEOF:
			return
		case sshtunnel.FrameResize:
			resizeSSHSession(session, payload)
		}
	}
}

func resizeSSHSession(session *ssh.Session, payload []byte) {
	var resize sshtunnel.Resize
	if err := json.Unmarshal(payload, &resize); err != nil {
		return
	}

	if resize.Width <= 0 || resize.Height <= 0 {
		return
	}

	_ = session.WindowChange(resize.Height, resize.Width)
}

func waitSSHSession(ctx context.Context, session *ssh.Session, client *ssh.Client) error {
	waitErr := make(chan error, 1)
	go func() {
		waitErr <- session.Wait()
	}()

	select {
	case err := <-waitErr:
		return err
	case <-ctx.Done():
		_ = session.Close()
		_ = client.Close()

		return ctx.Err()
	}
}

func writeSSHExit(writer *sshtunnel.FrameWriter, err error) error {
	status := sshtunnel.ExitStatus{}

	if err != nil {
		var exitErr *ssh.ExitError
		if !errors.As(err, &exitErr) {
			return err
		}

		status.Code = exitErr.ExitStatus()
	}

	payload, marshalErr := json.Marshal(status)
	if marshalErr != nil {
		return marshalErr
	}

	return writer.WriteFrame(sshtunnel.FrameExit, payload)
}
