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

// ErrVMInitFailed marks valid VM launches that failed during guest lifecycle actions.
var ErrVMInitFailed = errors.New("vm init failed")

const (
	actionPhaseInit  = "init"
	actionPhaseStart = "start"
)

type templateConfig struct {
	Resources templateResources `json:"resources"`
	Actions   templateActions   `json:"actions"`
}

type templateResources struct {
	VCPU   *int   `json:"vcpu,omitempty"`
	Memory *int64 `json:"memory,omitempty"`
	Volume *int64 `json:"volume,omitempty"`
}

type resolvedResources struct {
	cpus        int
	memoryBytes int64
	rootfsSize  string
}

type templateActions struct {
	Init  []templateAction `json:"init"`
	Start []templateAction `json:"start,omitempty"`
}

type templateAction struct {
	Run              string         `json:"run,omitempty"`
	WorkingDirectory string         `json:"working_directory,omitempty"`
	Use              string         `json:"use,omitempty"`
	With             map[string]any `json:"with,omitempty"`
}

func (m Manager) runInitActions(ctx context.Context, vm VM, config json.RawMessage, logs io.Writer) error {
	return m.runActions(ctx, vm, config, actionPhaseInit, logs)
}

func (m Manager) runStartActions(ctx context.Context, vm VM, config json.RawMessage, logs io.Writer) error {
	return m.runActions(ctx, vm, config, actionPhaseStart, logs)
}

func parseActions(config json.RawMessage, phase string) ([]templateAction, error) {
	parsed, err := parseTemplateConfig(config)
	if err != nil {
		return nil, err
	}

	switch phase {
	case actionPhaseInit:
		return parsed.Actions.Init, nil
	case actionPhaseStart:
		return parsed.Actions.Start, nil
	default:
		return nil, fmt.Errorf("unknown action phase %q", phase)
	}
}

func parseTemplateResources(config json.RawMessage) (templateResources, error) {
	parsed, err := parseTemplateConfig(config)
	if err != nil {
		return templateResources{}, err
	}

	if err := parsed.Resources.validate(); err != nil {
		return templateResources{}, err
	}

	return parsed.Resources, nil
}

func resolveTemplateResources(config json.RawMessage) (resolvedResources, error) {
	resources, err := parseTemplateResources(config)
	if err != nil {
		return resolvedResources{}, err
	}

	return resources.resolve()
}

func parseTemplateConfig(config json.RawMessage) (templateConfig, error) {
	if len(config) == 0 {
		return templateConfig{}, nil
	}

	var parsed templateConfig

	decoder := json.NewDecoder(bytes.NewReader(config))
	decoder.UseNumber()

	if err := decoder.Decode(&parsed); err != nil {
		return templateConfig{}, fmt.Errorf("parse template config: %w", err)
	}

	return parsed, nil
}

func (r templateResources) validate() error {
	if r.VCPU != nil && *r.VCPU < 1 {
		return errors.New("template resource vcpu must be at least 1")
	}

	if r.Memory != nil && *r.Memory < 1 {
		return errors.New("template resource memory must be at least 1 GiB")
	}

	if r.Volume != nil && *r.Volume < 1 {
		return errors.New("template resource volume must be at least 1 GiB")
	}

	return nil
}

func (r templateResources) resolve() (resolvedResources, error) {
	cpus := vmCPUs()
	if r.VCPU != nil {
		cpus = *r.VCPU
	}

	memoryBytes := vmMemoryBytes()

	if r.Memory != nil {
		value, err := resourceGiBBytes(*r.Memory, "memory")
		if err != nil {
			return resolvedResources{}, err
		}

		memoryBytes = value
	}

	rootfsSize := defaultRootfsSize

	if r.Volume != nil {
		value, err := resourceGiBBytes(*r.Volume, "volume")
		if err != nil {
			return resolvedResources{}, err
		}

		rootfsSize = strconv.FormatInt(value, 10)
	}

	return resolvedResources{cpus: cpus, memoryBytes: memoryBytes, rootfsSize: rootfsSize}, nil
}

func resourceGiBBytes(value int64, name string) (int64, error) {
	const maxInt64 = int64(1<<63 - 1)

	if value > maxInt64/gibBytes {
		return 0, fmt.Errorf("template resource %s is too large", name)
	}

	return value * gibBytes, nil
}

func (m Manager) runActions(ctx context.Context, vm VM, config json.RawMessage, phase string, logs io.Writer) error {
	m = m.withDefaults()

	actions, err := parseActions(config, phase)
	if err != nil {
		return err
	}

	for index, action := range actions {
		if err := m.runAction(ctx, vm, phase, index+1, action, logs); err != nil {
			return actionError{phase: phase, index: index + 1, err: err}
		}
	}

	return nil
}

func (m Manager) runAction(ctx context.Context, vm VM, phase string, index int, action templateAction, logs io.Writer) error {
	switch {
	case action.Run != "":
		return m.runGuestCommand(ctx, vm, runActionCommand(action), logs)
	case action.Use != "":
		return m.runPresetAction(ctx, vm, phase, index, action, logs)
	default:
		return fmt.Errorf("%s action must define run or use", phase)
	}
}

func runActionCommand(action templateAction) string {
	if action.WorkingDirectory == "" {
		return action.Run
	}

	dir := shellQuote(action.WorkingDirectory)

	return "mkdir -p " + dir + " && cd " + dir + " && sh -c " + shellQuote(action.Run)
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

type actionError struct {
	phase string
	index int
	err   error
}

func (e actionError) Error() string {
	phase := e.phase
	if phase == "" {
		phase = actionPhaseInit
	}

	return fmt.Sprintf("%s action %d failed: %v", phase, e.index, e.err)
}

func (e actionError) Unwrap() error {
	return e.err
}

func (e actionError) Is(target error) bool {
	return target == ErrVMInitFailed
}

type initActionError = actionError

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
