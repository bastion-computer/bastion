package cloudhypervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestRunInitActionsRunsCommandsInOrder(t *testing.T) {
	t.Parallel()

	var got []string

	manager := Manager{
		stream: func(_ context.Context, _ io.Writer, name string, args ...string) error {
			if name != "ssh" {
				t.Fatalf("command name = %q, want ssh", name)
			}

			got = append(got, args[len(args)-1])

			return nil
		},
	}

	err := manager.runInitActions(context.Background(), testActionVM(), json.RawMessage(`{"actions":{"init":[{"run":"echo one"},{"run":"printf '%s' two"}]}}`), nil)
	if err != nil {
		t.Fatalf("run init actions: %v", err)
	}

	want := []string{
		"sh -c 'echo one'",
		"sh -c 'printf '\"'\"'%s'\"'\"' two'",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
}

func TestRunInitActionsRunsCommandInWorkingDirectory(t *testing.T) {
	t.Parallel()

	var got []string

	manager := Manager{
		stream: func(_ context.Context, _ io.Writer, name string, args ...string) error {
			if name != "ssh" {
				t.Fatalf("command name = %q, want ssh", name)
			}

			got = append(got, args[len(args)-1])

			return nil
		},
	}

	const (
		dir = "/workspace/project dir"
		run = `printf '%s' "$PWD" > pwd.txt`
	)

	err := manager.runInitActions(context.Background(), testActionVM(), json.RawMessage(`{"actions":{"init":[{"run":"printf '%s' \"$PWD\" > pwd.txt","working_directory":"/workspace/project dir"}]}}`), nil)
	if err != nil {
		t.Fatalf("run init actions: %v", err)
	}

	expectedGuestCommand := "mkdir -p " + shellQuote(dir) + " && cd " + shellQuote(dir) + " && sh -c " + shellQuote(run)
	want := "sh -c " + shellQuote(expectedGuestCommand)

	if len(got) != 1 || got[0] != want {
		t.Fatalf("commands = %#v, want %#v", got, []string{want})
	}
}

func TestRunInitActionsStreamsGuestCommandOutput(t *testing.T) {
	t.Parallel()

	manager := Manager{
		stream: func(_ context.Context, logs io.Writer, _ string, _ ...string) error {
			_, err := logs.Write([]byte("installing node\n"))

			return err
		},
	}

	var logs bytes.Buffer

	err := manager.runInitActions(context.Background(), testActionVM(), json.RawMessage(`{"actions":{"init":[{"run":"echo installing"}]}}`), &logs)
	if err != nil {
		t.Fatalf("run init actions: %v", err)
	}

	if logs.String() != "installing node\n" {
		t.Fatalf("logs = %q, want streamed guest command output", logs.String())
	}
}

func TestRunInitActionsReportsFailingActionIndex(t *testing.T) {
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

	err := manager.runInitActions(context.Background(), testActionVM(), json.RawMessage(`{"actions":{"init":[{"run":"echo one"},{"run":"false"},{"run":"echo three"}]}}`), nil)
	if err == nil || !strings.Contains(err.Error(), "init action 2 failed") {
		t.Fatalf("run init actions error = %v, want action 2 failure", err)
	}

	if !errors.Is(err, ErrVMInitFailed) {
		t.Fatalf("run init actions error = %v, want vm init failure", err)
	}

	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
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

	if calls[0].name != "ssh" || !strings.Contains(calls[0].args[len(calls[0].args)-1], "mkdir -p") || !strings.Contains(calls[0].args[len(calls[0].args)-1], guestActionsDir) {
		t.Fatalf("prepare guest directory call = %#v", calls[0])
	}

	if calls[1].name != "scp" || !containsArg(calls[1].args, "-r") || !containsArg(calls[1].args, SSHUser+"@10.241.0.2:"+guestActionsDir) {
		t.Fatalf("copy preset call = %#v", calls[1])
	}

	runCommand := calls[2].args[len(calls[2].args)-1]
	for _, want := range []string{"cd ", guestPresetActionDir(1, "setup_node"), ". ./" + presetInputEnvFileName, "rm -f ./" + presetInputEnvFileName, "sh ./install_node.sh"} {
		if !strings.Contains(runCommand, want) {
			t.Fatalf("preset run command = %q, want to contain %q", runCommand, want)
		}
	}

	if _, err := os.Stat(filepath.Join(vm.EnvDir, "actions", "init-1-setup_node", presetInputEnvFileName)); !errors.Is(err, os.ErrNotExist) {
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
