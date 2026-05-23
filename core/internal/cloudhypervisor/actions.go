package cloudhypervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ErrVMInitFailed marks valid VM launches that failed during guest initialization.
var ErrVMInitFailed = errors.New("vm init failed")

type templateConfig struct {
	Actions templateActions `json:"actions"`
}

type templateActions struct {
	Init []templateAction `json:"init"`
}

type templateAction struct {
	Run  string         `json:"run,omitempty"`
	Use  string         `json:"use,omitempty"`
	With map[string]any `json:"with,omitempty"`
}

func (m Manager) runInitActions(ctx context.Context, vm VM, config json.RawMessage, logs io.Writer) error {
	m = m.withDefaults()

	actions, err := parseInitActions(config)
	if err != nil {
		return err
	}

	for index, action := range actions {
		if err := m.runInitAction(ctx, vm, index+1, action, logs); err != nil {
			return initActionError{index: index + 1, err: err}
		}
	}

	return nil
}

func parseInitActions(config json.RawMessage) ([]templateAction, error) {
	if len(config) == 0 {
		return nil, nil
	}

	var parsed templateConfig

	decoder := json.NewDecoder(bytes.NewReader(config))
	decoder.UseNumber()

	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("parse template config: %w", err)
	}

	return parsed.Actions.Init, nil
}

func (m Manager) runInitAction(ctx context.Context, vm VM, index int, action templateAction, logs io.Writer) error {
	switch {
	case action.Run != "":
		return m.runGuestCommand(ctx, vm, action.Run, logs)
	case action.Use != "":
		return m.runPresetAction(ctx, vm, index, action, logs)
	default:
		return errors.New("init action must define run or use")
	}
}

func (m Manager) runGuestCommand(ctx context.Context, vm VM, command string, logs io.Writer) error {
	args, err := guestCommandArgs(vm, command)
	if err != nil {
		return err
	}

	if err := m.stream(ctx, logs, "ssh", args...); err != nil {
		return sanitizeGuestCommandError(err)
	}

	return nil
}

func guestCommandArgs(vm VM, command string) ([]string, error) {
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
		"-p", strconv.Itoa(port),
		user + "@" + vm.GuestIP,
		"sh -c " + shellQuote(command),
	}, nil
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}

	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

type initActionError struct {
	index int
	err   error
}

func (e initActionError) Error() string {
	return fmt.Sprintf("init action %d failed: %v", e.index, e.err)
}

func (e initActionError) Unwrap() error {
	return e.err
}

func (e initActionError) Is(target error) bool {
	return target == ErrVMInitFailed
}

type guestCommandError struct {
	message string
	err     error
}

func (e guestCommandError) Error() string {
	return "guest command failed: " + e.message
}

func (e guestCommandError) Unwrap() error {
	return e.err
}

func sanitizeGuestCommandError(err error) error {
	message := err.Error()
	if _, detail, ok := strings.Cut(message, " failed: "); ok {
		message = detail
	}

	message = strings.TrimSpace(message)
	if message == "" {
		message = "unknown error"
	}

	return guestCommandError{message: message, err: err}
}
