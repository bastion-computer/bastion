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
	Init []templateAction `json:"init"`
}

type templateAction struct {
	Run              string         `json:"run,omitempty"`
	WorkingDirectory string         `json:"working_directory,omitempty"`
	Use              string         `json:"use,omitempty"`
	With             map[string]any `json:"with,omitempty"`
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
	parsed, err := parseTemplateConfig(config)
	if err != nil {
		return nil, err
	}

	return parsed.Actions.Init, nil
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

func (m Manager) runInitAction(ctx context.Context, vm VM, index int, action templateAction, logs io.Writer) error {
	switch {
	case action.Run != "":
		return m.runGuestCommand(ctx, vm, runActionCommand(action), logs)
	case action.Use != "":
		return m.runPresetAction(ctx, vm, index, action, logs)
	default:
		return errors.New("init action must define run or use")
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
