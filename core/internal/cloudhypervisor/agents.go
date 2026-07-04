package cloudhypervisor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/bastion-computer/bastion/core/internal/opencodeasset"
)

const (
	openCodeServiceName      = "bastion-opencode.service"
	openCodeAgentWaitSeconds = 60
)

func (m Manager) setupTemplateAgents(ctx context.Context, vm VM, config json.RawMessage, logs io.Writer) error {
	agents, err := parseTemplateAgents(config)
	if err != nil {
		return err
	}

	if agents.OpenCode == nil {
		return nil
	}

	if err := m.setupOpenCodeAgent(ctx, vm, *agents.OpenCode, logs); err != nil {
		return agentError{name: AgentOpenCode, operation: "setup", err: err}
	}

	return nil
}

func (m Manager) startEnvironmentAgents(ctx context.Context, vm VM, config json.RawMessage, logs io.Writer) error {
	agents, err := parseTemplateAgents(config)
	if err != nil {
		return err
	}

	if agents.OpenCode == nil {
		return nil
	}

	if err := m.startOpenCodeAgent(ctx, vm, *agents.OpenCode, logs); err != nil {
		return agentError{name: AgentOpenCode, operation: "start", err: err}
	}

	return nil
}

func (m Manager) setupOpenCodeAgent(ctx context.Context, vm VM, agent templateOpenCodeAgent, logs io.Writer) error {
	m = m.withDefaults()

	assets, err := loadOpenCodeAssets(m.DataDir)
	if err != nil {
		return err
	}

	src := assets.openCode
	guestPath := openCodeTmpPath

	useArchive := assets.archive != ""
	if useArchive {
		src = assets.archive
		guestPath = openCodeArchiveTmpPath
	}

	args, err := scpGuestFileArgs(vm, src, guestPath)
	if err != nil {
		return err
	}

	if err := m.runForVM(ctx, vm, "scp", args...); err != nil {
		return sanitizeGuestCommandError(err)
	}

	command, err := openCodeSetupCommand(agent, useArchive)
	if err != nil {
		return err
	}

	return m.runGuestCommand(ctx, vm, command, logs)
}

func (m Manager) startOpenCodeAgent(ctx context.Context, vm VM, agent templateOpenCodeAgent, logs io.Writer) error {
	command, err := openCodeStartCommand(agent)
	if err != nil {
		return err
	}

	return m.runGuestCommand(ctx, vm, command, logs)
}

func openCodeSetupCommand(agent templateOpenCodeAgent, useArchive bool) (string, error) {
	port, err := openCodePort(agent)
	if err != nil {
		return "", err
	}

	workingDirectory := openCodeWorkingDirectory(agent)

	configJSON, hasConfig, err := optionalJSONObject(agent.Config)
	if err != nil {
		return "", fmt.Errorf("encode opencode config: %w", err)
	}

	authJSON, hasAuth, err := optionalJSONObject(agent.Auth)
	if err != nil {
		return "", fmt.Errorf("encode opencode auth: %w", err)
	}

	lines := []string{
		shellStrictMode,
		"export DEBIAN_FRONTEND=noninteractive",
		"apt-get update",
		"apt-get install -y --no-install-recommends bash ca-certificates curl jq tar gzip",
		"mkdir -p /usr/local/bin",
		"umask 077",
	}
	if useArchive {
		lines = append(lines,
			"tar -xzf "+openCodeArchiveTmpPath+" -C /tmp "+opencodeasset.BinaryName,
			"install -m 0755 "+openCodeTmpPath+" "+openCodeGuestPath,
			"rm -f "+openCodeArchiveTmpPath+" "+openCodeTmpPath,
		)
	} else {
		lines = append(lines,
			"install -m 0755 "+openCodeTmpPath+" "+openCodeGuestPath,
			"rm -f "+openCodeTmpPath,
		)
	}

	if hasConfig {
		lines = append(lines, writeOpenCodeJSONCommand("config", configJSON, "/root/.config/opencode", "/root/.config/opencode/opencode.json"))
	}

	if hasAuth {
		lines = append(lines, writeOpenCodeJSONCommand("auth", authJSON, "/root/.local/share/opencode", "/root/.local/share/opencode/auth.json"))
	}

	lines = append(lines,
		"mkdir -p "+shellQuote(workingDirectory),
		"printf %s "+shellQuote(openCodeSystemdUnit(workingDirectory, port))+" > /etc/systemd/system/"+openCodeServiceName,
		"systemctl daemon-reload",
		"systemctl enable "+openCodeServiceName,
		openCodeGuestPath+" --version",
	)

	return strings.Join(lines, "\n"), nil
}

func openCodeStartCommand(agent templateOpenCodeAgent) (string, error) {
	port, err := openCodePort(agent)
	if err != nil {
		return "", err
	}

	workingDirectory := openCodeWorkingDirectory(agent)

	configJSON, hasConfig, err := optionalJSONObject(agent.Config)
	if err != nil {
		return "", fmt.Errorf("encode opencode config: %w", err)
	}

	authJSON, hasAuth, err := optionalJSONObject(agent.Auth)
	if err != nil {
		return "", fmt.Errorf("encode opencode auth: %w", err)
	}

	lines := []string{
		shellStrictMode,
		"umask 077",
	}

	if hasConfig {
		lines = append(lines, writeOpenCodeJSONCommand("config", configJSON, "/root/.config/opencode", "/root/.config/opencode/opencode.json"))
	}

	if hasAuth {
		lines = append(lines, writeOpenCodeJSONCommand("auth", authJSON, "/root/.local/share/opencode", "/root/.local/share/opencode/auth.json"))
	}

	lines = append(lines,
		"mkdir -p "+shellQuote(workingDirectory),
		"printf %s "+shellQuote(openCodeSystemdUnit(workingDirectory, port))+" > /etc/systemd/system/"+openCodeServiceName,
		"systemctl daemon-reload",
		"systemctl restart "+openCodeServiceName,
		openCodeHealthWaitCommand(port),
	)

	return strings.Join(lines, "\n"), nil
}

func optionalJSONObject(value map[string]any) (string, bool, error) {
	if value == nil {
		return "", false, nil
	}

	contents, err := json.Marshal(value)
	if err != nil {
		return "", false, err
	}

	return string(contents), true, nil
}

func writeOpenCodeJSONCommand(name, contents, dir, file string) string {
	tmp := name + "_tmp"

	return strings.Join([]string{
		tmp + "=$(mktemp)",
		"printf %s " + shellQuote(contents) + " | jq -e 'if type == \"object\" then . else error(\"" + name + " must be a JSON object\") end' > \"$" + tmp + "\"",
		"mkdir -p " + shellQuote(dir),
		"install -m 600 \"$" + tmp + "\" " + shellQuote(file),
		"rm -f \"$" + tmp + "\"",
	}, "\n")
}

func openCodeSystemdUnit(workingDirectory string, port int) string {
	return fmt.Sprintf(`[Unit]
Description=Bastion OpenCode Agent Server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
Environment=HOME=/root
EnvironmentFile=-/etc/environment
WorkingDirectory=%s
ExecStart=%s serve --hostname 127.0.0.1 --port %d
Restart=always
RestartSec=2

[Install]
WantedBy=multi-user.target
`, systemdPath(workingDirectory), openCodeGuestPath, port)
}

func openCodeHealthWaitCommand(port int) string {
	url := "http://127.0.0.1:" + strconv.Itoa(port) + "/global/health"

	return strings.Join([]string{
		"for i in $(seq 1 " + strconv.Itoa(openCodeAgentWaitSeconds) + "); do",
		"  if curl -fsS --connect-timeout 1 --max-time 2 " + shellQuote(url) + " 2>/dev/null | jq -e '.healthy == true' >/dev/null 2>&1; then exit 0; fi",
		"  sleep 1",
		"done",
		"systemctl status --no-pager " + openCodeServiceName + " >&2 || true",
		"journalctl -u " + openCodeServiceName + " --no-pager -n 50 >&2 || true",
		"exit 1",
	}, "\n")
}

func openCodeWorkingDirectory(agent templateOpenCodeAgent) string {
	if agent.WorkingDirectory == "" {
		return "/root"
	}

	return agent.WorkingDirectory
}

func openCodePort(agent templateOpenCodeAgent) (int, error) {
	if agent.Config == nil {
		return OpenCodeDefaultPort, nil
	}

	serverValue, ok := agent.Config["server"]
	if !ok {
		return OpenCodeDefaultPort, nil
	}

	server, ok := serverValue.(map[string]any)
	if !ok {
		return 0, errors.New("opencode config server must be an object")
	}

	portValue, ok := server["port"]
	if !ok {
		return OpenCodeDefaultPort, nil
	}

	port, err := intValue(portValue)
	if err != nil {
		return 0, fmt.Errorf("opencode config server port: %w", err)
	}

	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("opencode config server port %d is out of range", port)
	}

	return port, nil
}

func intValue(value any) (int, error) {
	switch value := value.(type) {
	case json.Number:
		parsed, err := value.Int64()
		if err != nil {
			return 0, err
		}

		return int(parsed), nil
	case float64:
		if math.Trunc(value) != value {
			return 0, errors.New("must be an integer")
		}

		return int(value), nil
	case int:
		return value, nil
	case int64:
		return int(value), nil
	default:
		return 0, errors.New("must be an integer")
	}
}

func systemdPath(value string) string {
	var builder strings.Builder

	for i := range len(value) {
		char := value[i]

		switch {
		case char == '%':
			builder.WriteString("%%")
		case char == '\\':
			builder.WriteString("\\\\")
		case char <= ' ' || char == '"' || char == '\'':
			_, _ = fmt.Fprintf(&builder, "\\x%02x", char)
		default:
			builder.WriteByte(char)
		}
	}

	return builder.String()
}

type agentError struct {
	name      string
	operation string
	err       error
}

func (e agentError) Error() string {
	return fmt.Sprintf("agent %s %s failed: %v", e.name, e.operation, e.err)
}

func (e agentError) Unwrap() error {
	return e.err
}

func (e agentError) Is(target error) bool {
	return target == ErrVMInitFailed
}
