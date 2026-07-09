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

const (
	testSSHCommand             = "ssh"
	testSCPCommand             = "scp"
	testVersionInputName       = "version"
	testPythonVersionInputName = "python_version"
)

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

	if calls[1].name != testSCPCommand || !containsArg(calls[1].args, "-r") || !containsArg(calls[1].args, SSHUser+"@10.241.0.2:"+guestActionsDir) {
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

func TestRunActionsStagesPresetActionContext(t *testing.T) {
	t.Parallel()

	cases := []struct {
		phase    string
		config   json.RawMessage
		guestDir string
	}{
		{
			phase:    actionPhaseInit,
			config:   json.RawMessage(`{"actions":{"init":[{"use":"write_env_file","with":{"path":"/workspace/project"},"context":{"ALPHA":"one","COUNT":3,"NESTED":{"ok":true}}}]}}`),
			guestDir: guestPresetActionDir(actionPhaseInit, 1, "write_env_file"),
		},
		{
			phase:    actionPhaseStart,
			config:   json.RawMessage(`{"actions":{"init":[],"start":[{"use":"write_env_file","with":{"path":"/workspace/project"},"context":{"ALPHA":"one","COUNT":3,"NESTED":{"ok":true}}}]}}`),
			guestDir: guestPresetActionDir(actionPhaseStart, 1, "write_env_file"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.phase, func(t *testing.T) {
			t.Parallel()

			dataDir := t.TempDir()
			writeTestPresetAction(t, dataDir, "write_env_file", `{
  "inputs": {
    "path": {"type": "string", "required": true}
  },
  "run": "sh ./install_node.sh"
}`)

			captured := stagedPresetActionContext{}

			manager := Manager{
				DataDir: dataDir,
				stream:  recordPresetRunCommand(&captured),
				run:     recordPresetStagedFiles(t, &captured),
			}

			vm := testActionVM()
			vm.EnvDir = t.TempDir()

			if err := manager.runActions(context.Background(), vm, tc.config, tc.phase, nil); err != nil {
				t.Fatalf("run %s actions: %v", tc.phase, err)
			}

			stagedDir := filepath.Join(vm.EnvDir, "actions", presetActionDirName(tc.phase, 1, "write_env_file"))
			requirePresetActionContextStaged(t, captured, tc.guestDir)
			requireHostPresetStagedFilesRemoved(t, stagedDir)
		})
	}
}

type stagedPresetActionContext struct {
	inputEnv    []byte
	contextJSON []byte
	runCommand  string
}

func recordPresetRunCommand(captured *stagedPresetActionContext) func(context.Context, io.Writer, string, ...string) error {
	return func(_ context.Context, _ io.Writer, _ string, args ...string) error {
		captured.runCommand = args[len(args)-1]

		return nil
	}
}

func recordPresetStagedFiles(t *testing.T, captured *stagedPresetActionContext) func(context.Context, string, ...string) error {
	t.Helper()

	return func(_ context.Context, name string, args ...string) error {
		if name != testSCPCommand {
			return nil
		}

		srcDir := args[len(args)-2]

		inputEnv, err := os.ReadFile(filepath.Join(srcDir, presetInputEnvFileName)) //nolint:gosec // Test path is rooted in t.TempDir().
		if err != nil {
			t.Fatalf("read staged input env file: %v", err)
		}

		contextJSON, err := os.ReadFile(filepath.Join(srcDir, presetContextFileName)) //nolint:gosec // Test path is rooted in t.TempDir().
		if err != nil {
			t.Fatalf("read staged context file: %v", err)
		}

		captured.inputEnv = inputEnv
		captured.contextJSON = contextJSON

		return nil
	}
}

func requirePresetActionContextStaged(t *testing.T, captured stagedPresetActionContext, guestDir string) {
	t.Helper()

	input := string(captured.inputEnv)
	if !strings.Contains(input, "export BASTION_INPUT_PATH='/workspace/project'") {
		t.Fatalf("input env file = %q, want path input", input)
	}

	if !strings.Contains(input, "export BASTION_CONTEXT_FILE='"+guestDir+"/"+presetContextFileName+"'") {
		t.Fatalf("input env file = %q, want context file export", input)
	}

	var contextValue struct {
		Alpha  string `json:"ALPHA"`
		Count  int    `json:"COUNT"`
		Nested struct {
			OK bool `json:"ok"`
		} `json:"NESTED"`
	}
	if err := json.Unmarshal(captured.contextJSON, &contextValue); err != nil {
		t.Fatalf("unmarshal context JSON: %v", err)
	}

	if contextValue.Alpha != "one" || contextValue.Count != 3 || !contextValue.Nested.OK {
		t.Fatalf("context JSON = %s, want original action context", captured.contextJSON)
	}

	if !strings.Contains(captured.runCommand, "trap '") || !strings.Contains(captured.runCommand, presetContextFileName) {
		t.Fatalf("preset run command = %q, want context cleanup trap", captured.runCommand)
	}
}

func requireHostPresetStagedFilesRemoved(t *testing.T, stagedDir string) {
	t.Helper()

	if _, err := os.Stat(filepath.Join(stagedDir, presetInputEnvFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("host input env file error = %v, want not exist", err)
	}

	if _, err := os.Stat(filepath.Join(stagedDir, presetContextFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("host context file error = %v, want not exist", err)
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
			if name == testSCPCommand {
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

func TestSetupBunPresetInputs(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if err := builtinActions.Seed(dataDir); err != nil {
		t.Fatalf("seed actions: %v", err)
	}

	preset, err := loadPresetAction(dataDir, "setup_bun")
	if err != nil {
		t.Fatalf("load setup_bun preset: %v", err)
	}

	input, ok := preset.manifest.Inputs[testVersionInputName]
	if !ok {
		t.Fatalf("setup_bun input version is not defined: %#v", preset.manifest.Inputs)
	}

	if len(preset.manifest.Inputs) != 1 || input.Type != presetInputTypeString || input.Required {
		t.Fatalf("setup_bun inputs = %#v, want optional string version", preset.manifest.Inputs)
	}

	if err := validatePresetActionInputs(preset, nil); err != nil {
		t.Fatalf("validate setup_bun inputs without version: %v", err)
	}

	if err := validatePresetActionInputs(preset, map[string]any{testVersionInputName: "bun-v1.3.3"}); err != nil {
		t.Fatalf("validate setup_bun inputs with version: %v", err)
	}

	if err := validatePresetActionInputs(preset, map[string]any{testVersionInputName: 1.3}); err == nil || !strings.Contains(err.Error(), "input version: must be a string") {
		t.Fatalf("validate setup_bun invalid version error = %v, want type mismatch", err)
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

func TestSetupUVPresetInputs(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if err := builtinActions.Seed(dataDir); err != nil {
		t.Fatalf("seed actions: %v", err)
	}

	preset, err := loadPresetAction(dataDir, "setup_uv")
	if err != nil {
		t.Fatalf("load setup_uv preset: %v", err)
	}

	expected := []string{testVersionInputName, testPythonVersionInputName}
	if len(preset.manifest.Inputs) != len(expected) {
		t.Fatalf("setup_uv input count = %d, want %d: %#v", len(preset.manifest.Inputs), len(expected), preset.manifest.Inputs)
	}

	for _, name := range expected {
		input, ok := preset.manifest.Inputs[name]
		if !ok {
			t.Fatalf("setup_uv input %s is not defined: %#v", name, preset.manifest.Inputs)
		}

		if input.Type != presetInputTypeString || input.Required {
			t.Fatalf("setup_uv input %s = %#v, want optional string", name, input)
		}
	}

	if preset.manifest.Run != "sh ./install_uv.sh" {
		t.Fatalf("setup_uv run = %q, want install_uv script", preset.manifest.Run)
	}

	if err := validatePresetActionInputs(preset, nil); err != nil {
		t.Fatalf("validate setup_uv inputs without values: %v", err)
	}

	if err := validatePresetActionInputs(preset, map[string]any{testVersionInputName: "0.11.25", testPythonVersionInputName: "3.13"}); err != nil {
		t.Fatalf("validate setup_uv inputs with values: %v", err)
	}

	if err := validatePresetActionInputs(preset, map[string]any{testPythonVersionInputName: 3.13}); err == nil || !strings.Contains(err.Error(), "input python_version: must be a string") {
		t.Fatalf("validate setup_uv invalid python_version error = %v, want type mismatch", err)
	}
}

func TestSetupOpenJDKPresetInputs(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if err := builtinActions.Seed(dataDir); err != nil {
		t.Fatalf("seed actions: %v", err)
	}

	preset, err := loadPresetAction(dataDir, "setup_openjdk")
	if err != nil {
		t.Fatalf("load setup_openjdk preset: %v", err)
	}

	input, ok := preset.manifest.Inputs[testVersionInputName]
	if !ok {
		t.Fatalf("setup_openjdk input version is not defined: %#v", preset.manifest.Inputs)
	}

	if len(preset.manifest.Inputs) != 1 || input.Type != presetInputTypeNumber || input.Required {
		t.Fatalf("setup_openjdk inputs = %#v, want optional number version", preset.manifest.Inputs)
	}

	if preset.manifest.Run != "sh ./install_openjdk.sh" {
		t.Fatalf("setup_openjdk run = %q, want install_openjdk script", preset.manifest.Run)
	}

	if err := validatePresetActionInputs(preset, nil); err != nil {
		t.Fatalf("validate setup_openjdk inputs without version: %v", err)
	}

	if err := validatePresetActionInputs(preset, map[string]any{testVersionInputName: float64(21)}); err != nil {
		t.Fatalf("validate setup_openjdk inputs with version: %v", err)
	}

	if err := validatePresetActionInputs(preset, map[string]any{testVersionInputName: "21"}); err == nil || !strings.Contains(err.Error(), "input version: must be a number") {
		t.Fatalf("validate setup_openjdk invalid version error = %v, want type mismatch", err)
	}
}

func TestSetupAndroidSDKPresetInputs(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if err := builtinActions.Seed(dataDir); err != nil {
		t.Fatalf("seed actions: %v", err)
	}

	preset, err := loadPresetAction(dataDir, "setup_android_sdk")
	if err != nil {
		t.Fatalf("load setup_android_sdk preset: %v", err)
	}

	expected := map[string]string{
		"api_level":           presetInputTypeNumber,
		"avd_device":          presetInputTypeString,
		"avd_name":            presetInputTypeString,
		"avd_system_image":    presetInputTypeString,
		"build_tools_version": presetInputTypeString,
		"create_avd":          presetInputTypeBoolean,
		"extra_packages":      presetInputTypeString,
	}

	if len(preset.manifest.Inputs) != len(expected) {
		t.Fatalf("setup_android_sdk input count = %d, want %d: %#v", len(preset.manifest.Inputs), len(expected), preset.manifest.Inputs)
	}

	for name, wantType := range expected {
		input, ok := preset.manifest.Inputs[name]
		if !ok {
			t.Fatalf("setup_android_sdk input %s is not defined: %#v", name, preset.manifest.Inputs)
		}

		if input.Type != wantType || input.Required {
			t.Fatalf("setup_android_sdk input %s = %#v, want optional %s", name, input, wantType)
		}
	}

	if preset.manifest.Run != "sh ./install_android_sdk.sh" {
		t.Fatalf("setup_android_sdk run = %q, want install_android_sdk script", preset.manifest.Run)
	}

	if err := validatePresetActionInputs(preset, nil); err != nil {
		t.Fatalf("validate setup_android_sdk inputs without values: %v", err)
	}

	if err := validatePresetActionInputs(preset, map[string]any{
		"api_level":           float64(36),
		"avd_device":          "pixel_9",
		"avd_name":            "pixel_9",
		"avd_system_image":    "system-images;android-36;google_apis;x86_64",
		"build_tools_version": "36.0.0",
		"create_avd":          true,
		"extra_packages":      "system-images;android-36;google_apis;x86_64",
	}); err != nil {
		t.Fatalf("validate setup_android_sdk inputs with values: %v", err)
	}

	if err := validatePresetActionInputs(preset, map[string]any{"api_level": "36"}); err == nil || !strings.Contains(err.Error(), "input api_level: must be a number") {
		t.Fatalf("validate setup_android_sdk invalid api_level error = %v, want type mismatch", err)
	}
}

func TestSetupAndroidSDKPresetRejectsInvalidCreateAVDInput(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if err := builtinActions.Seed(dataDir); err != nil {
		t.Fatalf("seed actions: %v", err)
	}

	preset, err := loadPresetAction(dataDir, "setup_android_sdk")
	if err != nil {
		t.Fatalf("load setup_android_sdk preset: %v", err)
	}

	if err := validatePresetActionInputs(preset, map[string]any{"create_avd": "true"}); err == nil || !strings.Contains(err.Error(), "input create_avd: must be a boolean") {
		t.Fatalf("validate setup_android_sdk invalid create_avd error = %v, want type mismatch", err)
	}
}

func TestSetupMaestroPresetInputs(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if err := builtinActions.Seed(dataDir); err != nil {
		t.Fatalf("seed actions: %v", err)
	}

	preset, err := loadPresetAction(dataDir, "setup_maestro")
	if err != nil {
		t.Fatalf("load setup_maestro preset: %v", err)
	}

	input, ok := preset.manifest.Inputs[testVersionInputName]
	if !ok {
		t.Fatalf("setup_maestro input version is not defined: %#v", preset.manifest.Inputs)
	}

	if len(preset.manifest.Inputs) != 1 || input.Type != presetInputTypeString || input.Required {
		t.Fatalf("setup_maestro inputs = %#v, want optional string version", preset.manifest.Inputs)
	}

	if preset.manifest.Run != "sh ./install_maestro.sh" {
		t.Fatalf("setup_maestro run = %q, want install_maestro script", preset.manifest.Run)
	}

	if err := validatePresetActionInputs(preset, nil); err != nil {
		t.Fatalf("validate setup_maestro inputs without version: %v", err)
	}

	if err := validatePresetActionInputs(preset, map[string]any{testVersionInputName: "1.39.0"}); err != nil {
		t.Fatalf("validate setup_maestro inputs with version: %v", err)
	}

	if err := validatePresetActionInputs(preset, map[string]any{testVersionInputName: 1.39}); err == nil || !strings.Contains(err.Error(), "input version: must be a string") {
		t.Fatalf("validate setup_maestro invalid version error = %v, want type mismatch", err)
	}
}

func TestSetupAWSCLIPresetInputs(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if err := builtinActions.Seed(dataDir); err != nil {
		t.Fatalf("seed actions: %v", err)
	}

	preset, err := loadPresetAction(dataDir, "setup_aws_cli")
	if err != nil {
		t.Fatalf("load setup_aws_cli preset: %v", err)
	}

	expected := map[string]bool{
		"access_key_id":     true,
		"secret_access_key": true,
		"region":            true,
		"session_token":     false,
		"profile":           false,
		"output":            false,
	}

	if len(preset.manifest.Inputs) != len(expected) {
		t.Fatalf("setup_aws_cli input count = %d, want %d: %#v", len(preset.manifest.Inputs), len(expected), preset.manifest.Inputs)
	}

	for name, required := range expected {
		input, ok := preset.manifest.Inputs[name]
		if !ok {
			t.Fatalf("setup_aws_cli input %s is not defined: %#v", name, preset.manifest.Inputs)
		}

		if input.Type != presetInputTypeString || input.Required != required {
			t.Fatalf("setup_aws_cli input %s = %#v, want required=%t string", name, input, required)
		}
	}

	if preset.manifest.Run != "sh ./install_aws_cli.sh" {
		t.Fatalf("setup_aws_cli run = %q, want install_aws_cli script", preset.manifest.Run)
	}

	if err := validatePresetActionInputs(preset, map[string]any{
		"access_key_id":     "test-access-key-id",
		"secret_access_key": "test-secret-access-key",
		"region":            "us-west-2",
		"session_token":     "test-session-token",
		"profile":           "dev",
		"output":            "json",
	}); err != nil {
		t.Fatalf("validate setup_aws_cli inputs: %v", err)
	}

	if err := validatePresetActionInputs(preset, map[string]any{
		"access_key_id":     "test-access-key-id",
		"secret_access_key": "test-secret-access-key",
	}); err == nil || !strings.Contains(err.Error(), "input region is required") {
		t.Fatalf("validate missing region error = %v, want required region", err)
	}
}

func TestSetupDockerPresetInputs(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	if err := builtinActions.Seed(dataDir); err != nil {
		t.Fatalf("seed actions: %v", err)
	}

	preset, err := loadPresetAction(dataDir, "setup_docker")
	if err != nil {
		t.Fatalf("load setup_docker preset: %v", err)
	}

	if len(preset.manifest.Inputs) != 0 {
		t.Fatalf("setup_docker inputs = %#v, want none", preset.manifest.Inputs)
	}

	if preset.manifest.Run != "sh ./install_docker.sh" {
		t.Fatalf("setup_docker run = %q, want install_docker script", preset.manifest.Run)
	}

	if err := validatePresetActionInputs(preset, nil); err != nil {
		t.Fatalf("validate setup_docker inputs: %v", err)
	}
}

func TestSetupBaseAgentsCopiesOpenCodeAndInstallsBinary(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	writeTestOpenCodeAssets(t, dataDir)

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

	if err := manager.setupBaseAgents(context.Background(), testActionVM(), nil); err != nil {
		t.Fatalf("setup base agents: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("commands = %#v, want opencode copy and install command", calls)
	}

	if calls[0].name != testSCPCommand || !containsArg(calls[0].args, filepath.Join(dataDir, "opencode", "opencode-linux-x64.tar.gz")) || !containsArg(calls[0].args, SSHUser+"@10.241.0.2:/tmp/opencode-linux-x64.tar.gz") {
		t.Fatalf("opencode copy call = %#v, want scp from system archive to guest tmp", calls[0])
	}

	if calls[1].name != testSSHCommand {
		t.Fatalf("opencode install call name = %q, want %s", calls[1].name, testSSHCommand)
	}

	command := calls[1].args[len(calls[1].args)-1]
	for _, want := range []string{
		"apt-get install -y --no-install-recommends bash ca-certificates curl jq tar gzip",
		"tar -xzf /tmp/opencode-linux-x64.tar.gz -C /tmp opencode",
		"install -m 0755 /tmp/opencode /usr/local/bin/opencode",
		"rm -f /tmp/opencode-linux-x64.tar.gz /tmp/opencode",
		"/usr/local/bin/opencode --version",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("opencode base setup command = %q, want to contain %q", command, want)
		}
	}

	for _, unwanted := range []string{
		"/root/.config/opencode/opencode.json",
		"/root/.local/share/opencode/auth.json",
		"bastion-opencode.service",
	} {
		if strings.Contains(command, unwanted) {
			t.Fatalf("opencode base setup command = %q, want not to contain %q", command, unwanted)
		}
	}
}

func TestSetupTemplateAgentsWritesOpenCodeInputsWithoutInstallingBinary(t *testing.T) {
	t.Parallel()

	var commands []string

	manager := Manager{stream: recordSSHCommands(t, &commands)}
	config := json.RawMessage(`{"agents":{"opencode":{"working_directory":"/workspace/project dir","auth":{"anthropic":{"type":"api","key":"test-key"}},"config":{"model":"anthropic/claude-sonnet-4-5","server":{"port":4097}}}},"actions":{"init":[]}}`)

	if err := manager.setupTemplateAgents(context.Background(), testActionVM(), config, nil); err != nil {
		t.Fatalf("setup template agents: %v", err)
	}

	if len(commands) != 1 {
		t.Fatalf("commands = %#v, want opencode setup command only", commands)
	}

	command := commands[0]
	for _, want := range []string{
		"/root/.config/opencode/opencode.json",
		"/root/.local/share/opencode/auth.json",
		"anthropic/claude-sonnet-4-5",
		"test-key",
		"bastion-opencode.service",
		"/workspace/project dir",
		`WorkingDirectory=/workspace/project\x20dir`,
		"--port 4097",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("opencode template setup command = %q, want to contain %q", command, want)
		}
	}

	for _, unwanted := range []string{
		"apt-get update",
		"apt-get install",
		"tar -xzf /tmp/opencode-linux-x64.tar.gz",
		"install -m 0755 /tmp/opencode /usr/local/bin/opencode",
		"rm -f /tmp/opencode-linux-x64.tar.gz",
		"/usr/local/bin/opencode --version",
	} {
		if strings.Contains(command, unwanted) {
			t.Fatalf("opencode template setup command = %q, want not to contain %q", command, unwanted)
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

func TestOpenCodeStartCommandWaitsForHealthyAgent(t *testing.T) {
	t.Parallel()

	command, err := openCodeStartCommand(templateOpenCodeAgent{Config: map[string]any{"server": map[string]any{"port": 4097}}})
	if err != nil {
		t.Fatalf("opencode start command: %v", err)
	}

	for _, want := range []string{
		"http://127.0.0.1:4097/global/health",
		"jq -e '.healthy == true'",
	} {
		if !strings.Contains(command, want) {
			t.Fatalf("opencode start command = %q, want to contain %q", command, want)
		}
	}
}

func TestOpenCodeSystemdUnitImportsEnvironmentFile(t *testing.T) {
	t.Parallel()

	unit := openCodeSystemdUnit("/workspace/project", 4096)

	if !strings.Contains(unit, "EnvironmentFile=-/etc/environment") {
		t.Fatalf("opencode systemd unit = %q, want to import /etc/environment", unit)
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

func writeTestOpenCodeAssets(t *testing.T, dataDir string) {
	t.Helper()

	dir := filepath.Join(dataDir, "opencode")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("create opencode asset dir: %v", err)
	}

	assetPath := filepath.Join(dir, "opencode")
	if err := os.WriteFile(assetPath, []byte("test"), 0o600); err != nil {
		t.Fatalf("write opencode asset: %v", err)
	}

	if err := os.Chmod(assetPath, 0o755); err != nil { //nolint:gosec // Test fixture must be executable.
		t.Fatalf("chmod opencode asset: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "opencode-linux-x64.tar.gz"), []byte("archive"), 0o600); err != nil {
		t.Fatalf("write opencode archive: %v", err)
	}

	manifest := `{"version":"v1.17.13","architecture":"x86_64","opencode":"opencode","archive":"opencode-linux-x64.tar.gz"}`
	if err := os.WriteFile(filepath.Join(dir, manifestFileName), []byte(manifest), 0o600); err != nil {
		t.Fatalf("write opencode manifest: %v", err)
	}
}

func containsArg(args []string, want string) bool {
	return slices.Contains(args, want)
}
