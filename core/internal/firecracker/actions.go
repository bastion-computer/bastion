package firecracker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type templateConfig struct {
	Actions templateActions `json:"actions"`
}

type templateActions struct {
	Init []runAction `json:"init"`
}

type runAction struct {
	Run string `json:"run"`
}

func (m Manager) runInitActions(ctx context.Context, vm VM, config json.RawMessage) error {
	m = m.withDefaults()

	actions, err := parseInitActions(config)
	if err != nil {
		return err
	}

	for index, action := range actions {
		if err := m.runGuestCommand(ctx, vm, action.Run); err != nil {
			return fmt.Errorf("init action %d failed: %w", index+1, err)
		}
	}

	return nil
}

func parseInitActions(config json.RawMessage) ([]runAction, error) {
	if len(config) == 0 {
		return nil, nil
	}

	var parsed templateConfig
	if err := json.Unmarshal(config, &parsed); err != nil {
		return nil, fmt.Errorf("parse template config: %w", err)
	}

	return parsed.Actions.Init, nil
}

func (m Manager) runGuestCommand(ctx context.Context, vm VM, command string) error {
	args, err := guestCommandArgs(vm, command)
	if err != nil {
		return err
	}

	return m.run(ctx, "ssh", args...)
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
