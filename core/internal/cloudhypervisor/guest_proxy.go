package cloudhypervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/bastion-computer/bastion/core/internal/tunnel"
)

const (
	guestProxyBinaryName = "bastion-guest-proxy"
	guestProxyGuestPath  = "/usr/local/bin/" + guestProxyBinaryName
	guestProxyTmpPath    = "/tmp/" + guestProxyBinaryName
	guestProxyService    = "bastion-guest-proxy.service"
	guestProxyHealthPath = "/_bastion/health"
	guestProxyWait       = 60 * time.Second
	guestProxySetup      = "setup"
	guestProxyStart      = "start"
)

func (m Manager) setupTemplateGuestProxy(ctx context.Context, vm VM, logs io.Writer) error {
	m = m.withDefaults()

	src, err := m.guestProxyBinaryPath()
	if err != nil {
		return guestProxyError{operation: guestProxySetup, err: err}
	}

	if err := m.runGuestCommand(ctx, vm, "mkdir -p /usr/local/bin", logs); err != nil {
		return guestProxyError{operation: guestProxySetup, err: err}
	}

	args, err := scpGuestFileArgs(vm, src, guestProxyTmpPath)
	if err != nil {
		return guestProxyError{operation: guestProxySetup, err: err}
	}

	if err := m.run(ctx, "scp", args...); err != nil {
		return guestProxyError{operation: guestProxySetup, err: sanitizeGuestCommandError(err)}
	}

	command := strings.Join([]string{
		shellStrictMode,
		"install -m 0755 " + shellQuote(guestProxyTmpPath) + " " + shellQuote(guestProxyGuestPath),
		"rm -f " + shellQuote(guestProxyTmpPath),
		"printf %s " + shellQuote(guestProxySystemdUnit()) + " > /etc/systemd/system/" + guestProxyService,
		"systemctl daemon-reload",
		"systemctl enable " + guestProxyService,
	}, "\n")

	if err := m.runGuestCommand(ctx, vm, command, logs); err != nil {
		return guestProxyError{operation: guestProxySetup, err: err}
	}

	return nil
}

func (m Manager) startEnvironmentGuestProxy(ctx context.Context, vm VM, logs io.Writer) error {
	command := strings.Join([]string{
		shellStrictMode,
		"systemctl daemon-reload",
		"systemctl restart " + guestProxyService,
	}, "\n")

	if err := m.runGuestCommand(ctx, vm, command, logs); err != nil {
		return guestProxyError{operation: guestProxyStart, err: err}
	}

	if err := waitForGuestProxy(ctx, vm.VsockSocketPath); err != nil {
		return guestProxyError{operation: guestProxyStart, err: err}
	}

	return nil
}

func (m Manager) guestProxyBinaryPath() (string, error) {
	if m.GuestProxyPath != "" {
		return executableFile(m.GuestProxyPath)
	}

	candidates := make([]string, 0, 2)
	if executable, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(executable), guestProxyBinaryName))
	}

	if path, err := exec.LookPath(guestProxyBinaryName); err == nil {
		candidates = append(candidates, path)
	}

	for _, candidate := range candidates {
		path, err := executableFile(candidate)
		if err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("%s not found next to bastiond or on PATH", guestProxyBinaryName)
}

func executableFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}

	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable", path)
	}

	return path, nil
}

func scpGuestFileArgs(vm VM, src, guestPath string) ([]string, error) {
	if vm.GuestIP == "" {
		return nil, errors.New("guest ip is required")
	}

	if vm.SSHKeyPath == "" {
		return nil, errors.New("ssh key path is required")
	}

	port := vm.SSHPort
	if port == 0 {
		port = SSHPort
	}

	user := vm.SSHUser
	if user == "" {
		user = SSHUser
	}

	return []string{
		"-i", vm.SSHKeyPath,
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-P", strconv.Itoa(port),
		src,
		user + "@" + vm.GuestIP + ":" + guestPath,
	}, nil
}

func waitForGuestProxy(ctx context.Context, vsockSocketPath string) error {
	if vsockSocketPath == "" {
		return errors.New("vsock socket path is required")
	}

	client := &http.Client{Transport: guestProxyHealthTransport(vsockSocketPath)}
	deadline := time.Now().Add(guestProxyWait)

	var lastErr error

	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://bastion-guest-proxy"+guestProxyHealthPath, nil)
		if err != nil {
			return err
		}

		res, err := client.Do(req)
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return nil
			}

			lastErr = fmt.Errorf("guest proxy health returned %s", res.Status)
		} else {
			lastErr = err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}

	if lastErr != nil {
		return fmt.Errorf("timed out waiting for guest proxy on %s: %w", vsockSocketPath, lastErr)
	}

	return fmt.Errorf("timed out waiting for guest proxy on %s", vsockSocketPath)
}

func guestProxyHealthTransport(vsockSocketPath string) *http.Transport {
	return &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return tunnel.DialGuestProxy(ctx, vsockSocketPath)
	}}
}

func guestProxySystemdUnit() string {
	return fmt.Sprintf(`[Unit]
Description=Bastion Guest Tunnel Proxy
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=%s --port %d
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
`, guestProxyGuestPath, tunnel.GuestProxyVsockPort)
}

type guestProxyError struct {
	operation string
	err       error
}

func (e guestProxyError) Error() string {
	return fmt.Sprintf("guest proxy %s failed: %v", e.operation, e.err)
}

func (e guestProxyError) Unwrap() error {
	return e.err
}

func (e guestProxyError) Is(target error) bool {
	return target == ErrVMInitFailed
}
