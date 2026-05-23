package cloudhypervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	presetactions "github.com/bastion-computer/bastion/core/actions"
)

const (
	guestActionsDir        = "/opt/bastion/actions"
	presetInputEnvFileName = ".bastion-inputs.env"
	presetInputEnvPrefix   = "BASTION_INPUT_"
)

type presetActionPackage struct {
	name     string
	dir      string
	manifest presetActionManifest
}

type presetActionManifest struct {
	Inputs map[string]presetActionInput `json:"inputs,omitempty"`
	Run    string                       `json:"run"`
}

type presetActionInput struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

func (m Manager) runPresetAction(ctx context.Context, vm VM, index int, action templateAction, logs io.Writer) error {
	preset, err := loadPresetAction(m.DataDir, action.Use)
	if err != nil {
		return err
	}

	if err := validatePresetActionInputs(preset, action.With); err != nil {
		return err
	}

	stagedDir, err := stagePresetAction(vm, index, preset, action.With)
	if err != nil {
		return err
	}

	removeHostInputFile := func() error {
		if err := os.Remove(filepath.Join(stagedDir, presetInputEnvFileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove host preset action inputs: %w", err)
		}

		return nil
	}

	guestDir := guestPresetActionDir(index, preset.name)
	if err := m.copyPresetActionToGuest(ctx, vm, stagedDir, path.Dir(guestDir), logs); err != nil {
		return errors.Join(err, removeHostInputFile())
	}

	removeErr := removeHostInputFile()
	runErr := m.runGuestCommand(ctx, vm, presetActionCommand(guestDir, preset.manifest.Run), logs)

	return errors.Join(removeErr, runErr)
}

func loadPresetAction(dataDir, name string) (presetActionPackage, error) {
	if dataDir == "" {
		return presetActionPackage{}, errors.New("data dir is required")
	}

	if !validPresetActionName(name) {
		return presetActionPackage{}, fmt.Errorf("invalid preset action name %q", name)
	}

	dir := filepath.Join(dataDir, presetactions.DirName, name)
	manifestPath := filepath.Join(dir, manifestFileName)

	contents, err := os.ReadFile(manifestPath) //nolint:gosec // Preset action path is rooted in the configured Bastion data directory.
	if err != nil {
		return presetActionPackage{}, fmt.Errorf("read preset action %s manifest: %w", name, err)
	}

	var manifest presetActionManifest

	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&manifest); err != nil {
		return presetActionPackage{}, fmt.Errorf("parse preset action %s manifest: %w", name, err)
	}

	if err := validatePresetActionManifest(name, manifest); err != nil {
		return presetActionPackage{}, err
	}

	return presetActionPackage{name: name, dir: dir, manifest: manifest}, nil
}

func validatePresetActionManifest(name string, manifest presetActionManifest) error {
	if strings.TrimSpace(manifest.Run) == "" {
		return fmt.Errorf("preset action %s manifest run is required", name)
	}

	for inputName, input := range manifest.Inputs {
		if !validPresetInputName(inputName) {
			return fmt.Errorf("preset action %s manifest input name %q is invalid", name, inputName)
		}

		switch input.Type {
		case "string", "number", "boolean":
		default:
			return fmt.Errorf("preset action %s manifest input %s has invalid type %q", name, inputName, input.Type)
		}
	}

	return nil
}

func validatePresetActionInputs(preset presetActionPackage, with map[string]any) error {
	inputs := preset.manifest.Inputs
	if inputs == nil {
		inputs = map[string]presetActionInput{}
	}

	for name := range with {
		if _, ok := inputs[name]; !ok {
			return fmt.Errorf("preset action %s input %s is not defined", preset.name, name)
		}
	}

	for name, input := range inputs {
		value, ok := with[name]
		if !ok {
			if input.Required {
				return fmt.Errorf("preset action %s input %s is required", preset.name, name)
			}

			continue
		}

		if _, err := presetInputValueString(value, input.Type); err != nil {
			return fmt.Errorf("preset action %s input %s: %w", preset.name, name, err)
		}
	}

	return nil
}

func stagePresetAction(vm VM, index int, preset presetActionPackage, with map[string]any) (string, error) {
	if vm.EnvDir == "" {
		return "", errors.New("environment directory is required")
	}

	stagedDir := filepath.Join(vm.EnvDir, presetactions.DirName, presetActionDirName(index, preset.name))
	if err := os.RemoveAll(stagedDir); err != nil {
		return "", fmt.Errorf("remove stale preset action staging directory: %w", err)
	}

	if err := copyDir(preset.dir, stagedDir); err != nil {
		return "", fmt.Errorf("stage preset action %s: %w", preset.name, err)
	}

	envFile, err := presetInputEnvFile(preset, with)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(filepath.Join(stagedDir, presetInputEnvFileName), envFile, 0o600); err != nil {
		return "", fmt.Errorf("write preset action inputs: %w", err)
	}

	return stagedDir, nil
}

func presetInputEnvFile(preset presetActionPackage, with map[string]any) ([]byte, error) {
	keys := make([]string, 0, len(with))
	for key := range with {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	var buf bytes.Buffer

	for _, key := range keys {
		input := preset.manifest.Inputs[key]

		value, err := presetInputValueString(with[key], input.Type)
		if err != nil {
			return nil, fmt.Errorf("preset action %s input %s: %w", preset.name, key, err)
		}

		if _, err := fmt.Fprintf(&buf, "export %s=%s\n", presetInputEnvName(key), shellQuote(value)); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

func presetInputValueString(value any, inputType string) (string, error) {
	switch inputType {
	case "string":
		value, ok := value.(string)
		if !ok {
			return "", errors.New("must be a string")
		}

		return value, nil
	case "number":
		switch value := value.(type) {
		case json.Number:
			return value.String(), nil
		case float64:
			return strconv.FormatFloat(value, 'f', -1, 64), nil
		default:
			return "", errors.New("must be a number")
		}
	case "boolean":
		value, ok := value.(bool)
		if !ok {
			return "", errors.New("must be a boolean")
		}

		return strconv.FormatBool(value), nil
	default:
		return "", fmt.Errorf("unsupported input type %q", inputType)
	}
}

func (m Manager) copyPresetActionToGuest(ctx context.Context, vm VM, srcDir, guestParent string, logs io.Writer) error {
	if err := m.runGuestCommand(ctx, vm, "mkdir -p "+shellQuote(guestParent), logs); err != nil {
		return fmt.Errorf("prepare preset action guest directory: %w", err)
	}

	args, err := scpGuestArgs(vm, srcDir, guestParent)
	if err != nil {
		return err
	}

	if err := m.run(ctx, "scp", args...); err != nil {
		return fmt.Errorf("copy preset action to guest: %w", sanitizeGuestCommandError(err))
	}

	return nil
}

func scpGuestArgs(vm VM, srcDir, guestDir string) ([]string, error) {
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
		"-r", srcDir,
		user + "@" + vm.GuestIP + ":" + guestDir,
	}, nil
}

func presetActionCommand(guestDir, command string) string {
	return strings.Join([]string{
		"set -eu",
		"cd " + shellQuote(guestDir),
		"set -a",
		". ./" + presetInputEnvFileName,
		"set +a",
		"rm -f ./" + presetInputEnvFileName,
		command,
	}, "\n")
}

func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", filepath.Base(src))
	}

	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, rel)

		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}

		if entry.IsDir() {
			mode := entryInfo.Mode().Perm()
			if mode == 0 {
				mode = 0o750
			} else {
				mode |= 0o700
			}

			return os.MkdirAll(target, mode)
		}

		if !entryInfo.Mode().IsRegular() {
			return fmt.Errorf("preset action file %s is not regular", path)
		}

		contents, err := os.ReadFile(path) //nolint:gosec // Source path is rooted in a configured preset action directory.
		if err != nil {
			return err
		}

		mode := entryInfo.Mode().Perm()
		if mode == 0 {
			mode = 0o640
		} else {
			mode |= 0o600
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}

		return os.WriteFile(target, contents, mode) //nolint:gosec // Destination path is rooted in the generated environment staging directory.
	})
}

func guestPresetActionDir(index int, name string) string {
	return path.Join(guestActionsDir, presetActionDirName(index, name))
}

func presetActionDirName(index int, name string) string {
	return fmt.Sprintf("init-%d-%s", index, name)
}

func presetInputEnvName(name string) string {
	return presetInputEnvPrefix + strings.ToUpper(name)
}

func validPresetActionName(value string) bool {
	return validIdentifier(value, true)
}

func validPresetInputName(value string) bool {
	return validIdentifier(value, false)
}

func validIdentifier(value string, allowDash bool) bool {
	if value == "" || !asciiLetter(value[0]) {
		return false
	}

	for i := 1; i < len(value); i++ {
		char := value[i]
		if asciiLetter(char) || asciiDigit(char) || char == '_' || allowDash && char == '-' {
			continue
		}

		return false
	}

	return true
}

func asciiLetter(char byte) bool {
	return char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z'
}

func asciiDigit(char byte) bool {
	return char >= '0' && char <= '9'
}
