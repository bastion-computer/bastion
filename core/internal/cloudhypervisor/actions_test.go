package cloudhypervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	builtinActions "github.com/bastion-computer/bastion/core/actions"
)

const testSSHCommand = "ssh"

func TestRunActionsRunsCommandsInOrder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		phase  string
		config json.RawMessage
	}{
		{phase: actionPhaseInit, config: json.RawMessage(`{"actions":{"init":[{"run":"echo one"},{"run":"printf '%s' two"}]}}`)},
		{phase: actionPhaseStart, config: json.RawMessage(`{"actions":{"init":[{"run":"echo init"}],"start":[{"run":"echo one"},{"run":"printf '%s' two"}]}}`)},
	}

	for _, tc := range cases {
		t.Run(tc.phase, func(t *testing.T) {
			t.Parallel()

			var got []string

			manager := Manager{stream: recordSSHCommands(t, &got)}

			if err := runTestActions(manager, tc.phase, tc.config, nil); err != nil {
				t.Fatalf("run %s actions: %v", tc.phase, err)
			}

			want := []string{
				"sh -c 'echo one'",
				"sh -c 'printf '\"'\"'%s'\"'\"' two'",
			}
			if !slices.Equal(got, want) {
				t.Fatalf("commands = %#v, want %#v", got, want)
			}
		})
	}
}

func TestRunActionsRunsCommandInWorkingDirectory(t *testing.T) {
	t.Parallel()

	const (
		dir = "/workspace/project dir"
		run = `printf '%s' "$PWD" > pwd.txt`
	)

	cases := []struct {
		phase  string
		config json.RawMessage
	}{
		{phase: actionPhaseInit, config: json.RawMessage(`{"actions":{"init":[{"run":"printf '%s' \"$PWD\" > pwd.txt","working_directory":"/workspace/project dir"}]}}`)},
		{phase: actionPhaseStart, config: json.RawMessage(`{"actions":{"init":[],"start":[{"run":"printf '%s' \"$PWD\" > pwd.txt","working_directory":"/workspace/project dir"}]}}`)},
	}

	for _, tc := range cases {
		t.Run(tc.phase, func(t *testing.T) {
			t.Parallel()

			var got []string

			manager := Manager{stream: recordSSHCommands(t, &got)}

			if err := runTestActions(manager, tc.phase, tc.config, nil); err != nil {
				t.Fatalf("run %s actions: %v", tc.phase, err)
			}

			expectedGuestCommand := "mkdir -p " + shellQuote(dir) + " && cd " + shellQuote(dir) + " && sh -c " + shellQuote(run)
			want := "sh -c " + shellQuote(expectedGuestCommand)

			if len(got) != 1 || got[0] != want {
				t.Fatalf("commands = %#v, want %#v", got, []string{want})
			}
		})
	}
}

func TestRunActionsStreamsGuestCommandOutput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		phase  string
		config json.RawMessage
	}{
		{phase: actionPhaseInit, config: json.RawMessage(`{"actions":{"init":[{"run":"echo installing"}]}}`)},
		{phase: actionPhaseStart, config: json.RawMessage(`{"actions":{"init":[],"start":[{"run":"echo installing"}]}}`)},
	}

	for _, tc := range cases {
		t.Run(tc.phase, func(t *testing.T) {
			t.Parallel()

			manager := Manager{
				stream: func(_ context.Context, logs io.Writer, _ string, _ ...string) error {
					_, err := logs.Write([]byte("installing node\n"))

					return err
				},
			}

			var logs bytes.Buffer

			if err := runTestActions(manager, tc.phase, tc.config, &logs); err != nil {
				t.Fatalf("run %s actions: %v", tc.phase, err)
			}

			if logs.String() != "installing node\n" {
				t.Fatalf("logs = %q, want streamed guest command output", logs.String())
			}
		})
	}
}

func TestRunActionsReportFailingActionIndex(t *testing.T) {
	t.Parallel()

	cases := []struct {
		phase     string
		config    json.RawMessage
		wantError string
	}{
		{phase: actionPhaseInit, config: json.RawMessage(`{"actions":{"init":[{"run":"echo one"},{"run":"false"},{"run":"echo three"}]}}`), wantError: "init action 2 failed"},
		{phase: actionPhaseStart, config: json.RawMessage(`{"actions":{"init":[],"start":[{"run":"echo one"},{"run":"false"},{"run":"echo three"}]}}`), wantError: "start action 2 failed"},
	}

	for _, tc := range cases {
		t.Run(tc.phase, func(t *testing.T) {
			t.Parallel()

			calls := 0
			manager := Manager{
				stream: func(_ context.Context, _ io.Writer, _ string, _ ...string) error {
					calls++
					if calls == 2 {
						return errors.New("boom")
					}

					return nil
				},
			}

			err := runTestActions(manager, tc.phase, tc.config, nil)
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("run %s actions error = %v, want action 2 failure", tc.phase, err)
			}

			if !errors.Is(err, ErrVMInitFailed) {
				t.Fatalf("run %s actions error = %v, want vm action failure", tc.phase, err)
			}

			if calls != 2 {
				t.Fatalf("calls = %d, want 2", calls)
			}
		})
	}
}

func runTestActions(manager Manager, phase string, config json.RawMessage, logs io.Writer) error {
	switch phase {
	case actionPhaseInit:
		return manager.runInitActions(context.Background(), testActionVM(), config, logs)
	case actionPhaseStart:
		return manager.runStartActions(context.Background(), testActionVM(), config, logs)
	default:
		return manager.runActions(context.Background(), testActionVM(), config, phase, logs)
	}
}

func recordSSHCommands(t *testing.T, got *[]string) func(context.Context, io.Writer, string, ...string) error {
	t.Helper()

	return func(_ context.Context, _ io.Writer, name string, args ...string) error {
		if name != testSSHCommand {
			t.Fatalf("command name = %q, want %s", name, testSSHCommand)
		}

		*got = append(*got, args[len(args)-1])

		return nil
	}
}

func TestRunInitActionsSanitizesSSHWrapperFailure(t *testing.T) {
	t.Parallel()

	manager := Manager{
		stream: func(_ context.Context, _ io.Writer, _ string, _ ...string) error {
			return errors.New("ssh -i /secret/key -p 22 root@10.241.0.2 sh -c 'false' failed: exit status 1: intentional failure")
		},
	}

	err := manager.runInitActions(context.Background(), testActionVM(), json.RawMessage(`{"actions":{"init":[{"run":"false"}]}}`), nil)
	if err == nil {
		t.Fatal("run init actions error = nil, want failure")
	}

	if strings.Contains(err.Error(), "ssh -i") || strings.Contains(err.Error(), "/secret/key") {
		t.Fatalf("run init actions error leaks ssh wrapper: %v", err)
	}

	if !strings.Contains(err.Error(), "init action 1 failed") || !strings.Contains(err.Error(), "exit status 1: intentional failure") {
		t.Fatalf("run init actions error = %v, want sanitized failure detail", err)
	}
}

func TestRunInitActionsRunsPresetActionWithInputs(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	writeTestPresetAction(t, dataDir, "setup_node", `{
  "inputs": {
    "version": {"type": "number", "required": true}
  },
  "run": "sh ./install_node.sh"
}`)

	type call struct {
		name string
		args []string
	}

	var calls []call

	manager := Manager{
		DataDir: dataDir,
		stream: func(_ context.Context, _ io.Writer, name string, args ...string) error {
			calls = append(calls, call{name: name, args: append([]string(nil), args...)})

			return nil
		},
		run: func(_ context.Context, name string, args ...string) error {
			calls = append(calls, call{name: name, args: append([]string(nil), args...)})

			return nil
		},
	}

	vm := testActionVM()
	vm.EnvDir = t.TempDir()

	err := manager.runInitActions(context.Background(), vm, json.RawMessage(`{"actions":{"init":[{"use":"setup_node","with":{"version":24}}]}}`), nil)
	if err != nil {
		t.Fatalf("run init actions: %v", err)
	}

	if len(calls) != 3 {
		t.Fatalf("call count = %d, want 3: %#v", len(calls), calls)
	}

	if calls[0].name != testSSHCommand || !strings.Contains(calls[0].args[len(calls[0].args)-1], "mkdir -p") || !strings.Contains(calls[0].args[len(calls[0].args)-1], guestActionsDir) {
		t.Fatalf("prepare guest directory call = %#v", calls[0])
	}

	if calls[1].name != "scp" || !containsArg(calls[1].args, "-r") || !containsArg(calls[1].args, SSHUser+"@10.241.0.2:"+guestActionsDir) {
		t.Fatalf("copy preset call = %#v", calls[1])
	}

	runCommand := calls[2].args[len(calls[2].args)-1]
	for _, want := range []string{"cd ", guestPresetActionDir(actionPhaseInit, 1, "setup_node"), ". ./" + presetInputEnvFileName, "rm -f ./" + presetInputEnvFileName, "sh ./install_node.sh"} {
		if !strings.Contains(runCommand, want) {
			t.Fatalf("preset run command = %q, want to contain %q", runCommand, want)
		}
	}

	if _, err := os.Stat(filepath.Join(vm.EnvDir, "actions", "init-1-setup_node", presetInputEnvFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("host input env file error = %v, want not exist", err)
	}
}

func TestRunStartActionsRunsPresetActionWithInputs(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	writeTestPresetAction(t, dataDir, "setup_node", `{
  "inputs": {
    "version": {"type": "number", "required": true}
  },
  "run": "sh ./install_node.sh"
}`)

	type call struct {
		name string
		args []string
	}

	var calls []call

	manager := Manager{
		DataDir: dataDir,
		stream: func(_ context.Context, _ io.Writer, name string, args ...string) error {
			calls = append(calls, call{name: name, args: append([]string(nil), args...)})

			return nil
		},
		run: func(_ context.Context, name string, args ...string) error {
			calls = append(calls, call{name: name, args: append([]string(nil), args...)})

			return nil
		},
	}

	vm := testActionVM()
	vm.EnvDir = t.TempDir()

	err := manager.runStartActions(context.Background(), vm, json.RawMessage(`{"actions":{"init":[],"start":[{"use":"setup_node","with":{"version":24}}]}}`), nil)
	if err != nil {
		t.Fatalf("run start actions: %v", err)
	}

	if len(calls) != 3 {
		t.Fatalf("call count = %d, want 3: %#v", len(calls), calls)
	}

	runCommand := calls[2].args[len(calls[2].args)-1]
	if !strings.Contains(runCommand, path.Join(guestActionsDir, "start-1-setup_node")) {
		t.Fatalf("preset run command = %q, want start action guest directory", runCommand)
	}

	if _, err := os.Stat(filepath.Join(vm.EnvDir, "actions", "start-1-setup_node", presetInputEnvFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("host input env file error = %v, want not exist", err)
	}
}

func TestRunInitActionsRemovesHostPresetInputFileWhenCopyFails(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	writeTestPresetAction(t, dataDir, "setup_node", `{
  "inputs": {
    "version": {"type": "number", "required": true}
  },
  "run": "sh ./install_node.sh"
}`)

	manager := Manager{
		DataDir: dataDir,
		stream: func(_ context.Context, _ io.Writer, _ string, _ ...string) error {
			return nil
		},
		run: func(_ context.Context, name string, _ ...string) error {
			if name == "scp" {
				return errors.New("copy failed")
			}

			return nil
		},
	}

	vm := testActionVM()
	vm.EnvDir = t.TempDir()

	err := manager.runInitActions(context.Background(), vm, json.RawMessage(`{"actions":{"init":[{"use":"setup_node","with":{"version":24}}]}}`), nil)
	if err == nil || !strings.Contains(err.Error(), "copy preset action to guest") {
		t.Fatalf("run init actions error = %v, want copy failure", err)
	}

	if _, err := os.Stat(filepath.Join(vm.EnvDir, "actions", "init-1-setup_node", presetInputEnvFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("host input env file error = %v, want not exist", err)
	}
}

func TestRunInitActionsRejectsMissingPresetInput(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	writeTestPresetAction(t, dataDir, "setup_node", `{
  "inputs": {
    "version": {"type": "number", "required": true}
  },
  "run": "sh ./install_node.sh"
}`)

	calls := 0
	manager := Manager{
		DataDir: dataDir,
		stream: func(_ context.Context, _ io.Writer, _ string, _ ...string) error {
			calls++

			return nil
		},
		run: func(_ context.Context, _ string, _ ...string) error {
			calls++

			return nil
		},
	}

	vm := testActionVM()
	vm.EnvDir = t.TempDir()

	err := manager.runInitActions(context.Background(), vm, json.RawMessage(`{"actions":{"init":[{"use":"setup_node"}]}}`), nil)
	if err == nil || !strings.Contains(err.Error(), "preset action setup_node input version is required") {
		t.Fatalf("run init actions error = %v, want missing input", err)
	}

	if calls != 0 {
		t.Fatalf("calls = %d, want 0", calls)
	}
}

func TestRunInitActionsRejectsPresetInputTypeMismatch(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	writeTestPresetAction(t, dataDir, "setup_node", `{
  "inputs": {
    "version": {"type": "number", "required": true}
  },
  "run": "sh ./install_node.sh"
}`)

	manager := Manager{DataDir: dataDir}
	vm := testActionVM()
	vm.EnvDir = t.TempDir()

	err := manager.runInitActions(context.Background(), vm, json.RawMessage(`{"actions":{"init":[{"use":"setup_node","with":{"version":"24"}}]}}`), nil)
	if err == nil || !strings.Contains(err.Error(), "preset action setup_node input version: must be a number") {
		t.Fatalf("run init actions error = %v, want type mismatch", err)
	}
}

func TestSetupGitHubCLIPresetInputs(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if err := builtinActions.Seed(dataDir); err != nil {
		t.Fatalf("seed actions: %v", err)
	}

	preset, err := loadPresetAction(dataDir, "setup_github_cli")
	if err != nil {
		t.Fatalf("load setup_github_cli preset: %v", err)
	}

	expected := map[string]bool{
		"token":        true,
		"hostname":     false,
		"git_protocol": false,
		"name":         false,
		"email":        false,
	}

	if len(preset.manifest.Inputs) != len(expected) {
		t.Fatalf("setup_github_cli input count = %d, want %d: %#v", len(preset.manifest.Inputs), len(expected), preset.manifest.Inputs)
	}

	for name, required := range expected {
		input, ok := preset.manifest.Inputs[name]
		if !ok {
			t.Fatalf("setup_github_cli input %s is not defined: %#v", name, preset.manifest.Inputs)
		}

		if input.Type != presetInputTypeString || input.Required != required {
			t.Fatalf("setup_github_cli input %s = %#v, want required=%t string", name, input, required)
		}
	}

	if err := validatePresetActionInputs(preset, map[string]any{
		"token":        "test-token",
		"hostname":     "github.com",
		"git_protocol": "https",
		"name":         "custom-agent",
		"email":        "custom-agent@example.com",
	}); err != nil {
		t.Fatalf("validate setup_github_cli inputs: %v", err)
	}

	if err := validatePresetActionInputs(preset, map[string]any{"name": "custom-agent"}); err == nil || !strings.Contains(err.Error(), "input token is required") {
		t.Fatalf("validate missing token error = %v, want required token", err)
	}
}

func TestSetupTemplateAgentsInstallsOpenCodeAndWritesInputs(t *testing.T) {
	t.Parallel()

	var commands []string

	manager := Manager{stream: recordSSHCommands(t, &commands)}

	config := json.RawMessage(`{"agents":{"opencode":{"working_directory":"/workspace/project dir","auth":{"anthropic":{"type":"api","key":"test-key"}},"config":{"model":"anthropic/claude-sonnet-4-5","server":{"port":4097}}}},"actions":{"init":[]}}`)

	if err := manager.setupTemplateAgents(context.Background(), testActionVM(), config, nil); err != nil {
		t.Fatalf("setup template agents: %v", err)
	}

	if len(commands) != 1 {
		t.Fatalf("commands = %#v, want one opencode setup command", commands)
	}

	for _, want := range []string{
		"https://opencode.ai/install",
		"/root/.config/opencode/opencode.json",
		"/root/.local/share/opencode/auth.json",
		"anthropic/claude-sonnet-4-5",
		"test-key",
		"bastion-opencode.service",
		"/workspace/project dir",
		`WorkingDirectory=/workspace/project\x20dir`,
		"--port 4097",
	} {
		if !strings.Contains(commands[0], want) {
			t.Fatalf("opencode setup command = %q, want to contain %q", commands[0], want)
		}
	}
}

func TestStartEnvironmentAgentsRestartsOpenCodeBeforeStartActions(t *testing.T) {
	t.Parallel()

	var commands []string

	manager := Manager{stream: recordSSHCommands(t, &commands)}

	config := json.RawMessage(`{"agents":{"opencode":{"config":{"server":{"port":4097}}}},"actions":{"init":[],"start":[{"run":"opencode --version"}]}}`)

	if err := manager.startEnvironmentAgents(context.Background(), testActionVM(), config, nil); err != nil {
		t.Fatalf("start environment agents: %v", err)
	}

	if err := manager.runStartActions(context.Background(), testActionVM(), config, nil); err != nil {
		t.Fatalf("run start actions: %v", err)
	}

	if len(commands) != 2 {
		t.Fatalf("commands = %#v, want agent restart then start action", commands)
	}

	if !strings.Contains(commands[0], "systemctl restart bastion-opencode.service") || !strings.Contains(commands[0], "127.0.0.1:4097/") {
		t.Fatalf("first command = %q, want opencode service restart and health wait", commands[0])
	}

	if !strings.Contains(commands[1], "opencode --version") {
		t.Fatalf("second command = %q, want start action after agent restart", commands[1])
	}
}

func TestLoadPresetActionRejectsInvalidManifest(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	writeTestPresetAction(t, dataDir, "bad_action", `{
  "inputs": {
    "bad-name": {"type": "string"}
  },
  "run": "sh ./bad.sh"
}`)

	_, err := loadPresetAction(dataDir, "bad_action")
	if err == nil || !strings.Contains(err.Error(), "manifest input name \"bad-name\" is invalid") {
		t.Fatalf("load preset action error = %v, want invalid input name", err)
	}
}

func TestResolveTemplateResourcesUsesTemplateValuesInGiB(t *testing.T) {
	t.Parallel()

	resources, err := parseTemplateResources(json.RawMessage(`{"resources":{"vcpu":3,"memory":4,"volume":5},"actions":{"init":[]}}`))
	if err != nil {
		t.Fatalf("parse template resources: %v", err)
	}

	resolved, err := resources.resolve()
	if err != nil {
		t.Fatalf("resolve template resources: %v", err)
	}

	if resolved.cpus != 3 || resolved.memoryBytes != 4*gibBytes || resolved.rootfsSize != strconv.FormatInt(5*gibBytes, 10) {
		t.Fatalf("resolved resources = %#v, want 3 cpu, 4 GiB memory, 5 GiB rootfs", resolved)
	}
}

func TestResolveTemplateResourcesUsesRuntimeDefaults(t *testing.T) {
	t.Setenv(vmCPUsEnv, "7")
	t.Setenv(vmMemoryBytesEnv, "12345")

	resources, err := parseTemplateResources(json.RawMessage(`{"actions":{"init":[]}}`))
	if err != nil {
		t.Fatalf("parse template resources: %v", err)
	}

	resolved, err := resources.resolve()
	if err != nil {
		t.Fatalf("resolve template resources: %v", err)
	}

	if resolved.cpus != 7 || resolved.memoryBytes != 12345 || resolved.rootfsSize != defaultRootfsSize {
		t.Fatalf("resolved default resources = %#v, want runtime defaults", resolved)
	}
}

func TestFailVMWritesErrorState(t *testing.T) {
	t.Parallel()

	cause := errors.New("init action 1 failed")

	failed, err := failVM(VM{EnvironmentID: testEnvironmentID, EnvDir: t.TempDir()}, cause)
	if !errors.Is(err, cause) {
		t.Fatalf("fail vm error = %v, want %v", err, cause)
	}

	if failed.State != StateError || failed.LastError != cause.Error() {
		t.Fatalf("failed vm = %#v, want error state", failed)
	}

	stored, err := readVMState(failed.EnvDir)
	if err != nil {
		t.Fatalf("read vm state: %v", err)
	}

	if stored.State != StateError || stored.LastError != cause.Error() {
		t.Fatalf("stored vm = %#v, want error state", stored)
	}
}

func testActionVM() VM {
	return VM{
		GuestIP:    "10.241.0.2",
		SSHUser:    SSHUser,
		SSHPort:    SSHPort,
		SSHKeyPath: "/tmp/test.id_rsa",
	}
}

func writeTestPresetAction(t *testing.T, dataDir, name, manifest string) {
	t.Helper()

	dir := filepath.Join(dataDir, "actions", name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("create preset action dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, manifestFileName), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write preset action manifest: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "install_node.sh"), []byte("#!/usr/bin/env sh\n"), 0o600); err != nil {
		t.Fatalf("write preset action script: %v", err)
	}
}

func containsArg(args []string, want string) bool {
	return slices.Contains(args, want)
}
