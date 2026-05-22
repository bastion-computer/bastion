package firecracker

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRunInitActionsRunsCommandsInOrder(t *testing.T) {
	t.Parallel()

	var got []string

	manager := Manager{
		run: func(_ context.Context, name string, args ...string) error {
			if name != "ssh" {
				t.Fatalf("command name = %q, want ssh", name)
			}

			got = append(got, args[len(args)-1])

			return nil
		},
	}

	err := manager.runInitActions(context.Background(), testActionVM(), json.RawMessage(`{"actions":{"init":[{"run":"echo one"},{"run":"printf '%s' two"}]}}`))
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

func TestRunInitActionsReportsFailingActionIndex(t *testing.T) {
	t.Parallel()

	calls := 0
	manager := Manager{
		run: func(_ context.Context, _ string, _ ...string) error {
			calls++
			if calls == 2 {
				return errors.New("boom")
			}

			return nil
		},
	}

	err := manager.runInitActions(context.Background(), testActionVM(), json.RawMessage(`{"actions":{"init":[{"run":"echo one"},{"run":"false"},{"run":"echo three"}]}}`))
	if err == nil || !strings.Contains(err.Error(), "init action 2 failed") {
		t.Fatalf("run init actions error = %v, want action 2 failure", err)
	}

	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestFailVMWritesErrorState(t *testing.T) {
	t.Parallel()

	cause := errors.New("init action 1 failed")

	failed, err := failVM(VM{EnvironmentID: "env_test", EnvDir: t.TempDir()}, cause)
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
