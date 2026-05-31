package cloudhypervisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bastion-computer/bastion/core/internal/config"
)

const (
	functionsDir          = "functions"
	guestFunctionsDir     = "/opt/bastion/functions"
	functionInputsFile    = ".bastion-inputs.json"
	functionWorkerFile    = ".bastion-worker.ts"
	functionRuntimeAction = "setup_bun"
)

type functionPackage struct {
	name     string
	dir      string
	manifest functionManifest
}

type functionManifest struct {
	Inputs  map[string]presetActionInput `json:"inputs,omitempty"`
	Handler string                       `json:"handler"`
}

func (m Manager) startFunctionWorkers(ctx context.Context, vm VM, config json.RawMessage, logs io.Writer) error {
	functions, err := parseTemplateFunctions(config)
	if err != nil {
		return err
	}

	if len(functions) == 0 {
		return nil
	}

	if err := m.runPresetAction(ctx, vm, 0, templateAction{Use: functionRuntimeAction}, logs); err != nil {
		return fmt.Errorf("install function runtime: %w", err)
	}

	for name, function := range functions {
		if err := m.startFunctionWorker(ctx, vm, name, function, logs); err != nil {
			return fmt.Errorf("start function %s worker: %w", name, err)
		}
	}

	return nil
}

func (m Manager) startFunctionWorker(ctx context.Context, vm VM, name string, function templateFunction, logs io.Writer) error {
	pkg, err := loadFunctionPackage(m.DataDir, name)
	if err != nil {
		return err
	}

	if err := validateFunctionInputs(pkg, function.With); err != nil {
		return err
	}

	stagedDir, err := stageFunction(vm, pkg, function)
	if err != nil {
		return err
	}

	guestDir := path.Join(guestFunctionsDir, pkg.name)
	if err := m.copyFunctionToGuest(ctx, vm, stagedDir, path.Dir(guestDir), logs); err != nil {
		return err
	}

	return m.runGuestCommand(ctx, vm, functionWorkerCommand(guestDir, pkg.name), logs)
}

func loadFunctionPackage(dataDir, name string) (functionPackage, error) {
	if dataDir == "" {
		return functionPackage{}, errors.New("data dir is required")
	}

	if !validPresetActionName(name) {
		return functionPackage{}, fmt.Errorf("invalid function name %q", name)
	}

	dir := filepath.Join(dataDir, functionsDir, name)
	manifestPath := filepath.Join(dir, manifestFileName)

	contents, err := os.ReadFile(manifestPath) //nolint:gosec // Function package path is rooted in the configured Bastion data directory.
	if err != nil {
		return functionPackage{}, fmt.Errorf("read function %s manifest: %w", name, err)
	}

	var manifest functionManifest

	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&manifest); err != nil {
		return functionPackage{}, fmt.Errorf("parse function %s manifest: %w", name, err)
	}

	if err := validateFunctionManifest(name, manifest); err != nil {
		return functionPackage{}, err
	}

	return functionPackage{name: name, dir: dir, manifest: manifest}, nil
}

func validateFunctionManifest(name string, manifest functionManifest) error {
	if strings.TrimSpace(manifest.Handler) == "" {
		return fmt.Errorf("function %s manifest handler is required", name)
	}

	if err := validateFunctionHandlerPath(name, manifest.Handler); err != nil {
		return err
	}

	for inputName, input := range manifest.Inputs {
		if !validPresetInputName(inputName) {
			return fmt.Errorf("function %s manifest input name %q is invalid", name, inputName)
		}

		switch input.Type {
		case "string", "number", "boolean":
		default:
			return fmt.Errorf("function %s manifest input %s has invalid type %q", name, inputName, input.Type)
		}
	}

	return nil
}

func validateFunctionHandlerPath(name, handler string) error {
	clean := path.Clean(strings.TrimSpace(filepath.ToSlash(handler)))
	if clean == "." || strings.HasPrefix(clean, "../") || clean == ".." || strings.HasPrefix(clean, "/") {
		return fmt.Errorf("function %s manifest handler path is invalid", name)
	}

	return nil
}

func validateFunctionInputs(pkg functionPackage, with map[string]any) error {
	return validatePackageInputs("function "+pkg.name, pkg.manifest.Inputs, with)
}

func stageFunction(vm VM, pkg functionPackage, function templateFunction) (string, error) {
	if vm.EnvDir == "" {
		return "", errors.New("environment directory is required")
	}

	stagedDir := filepath.Join(vm.EnvDir, functionsDir, pkg.name)
	if err := os.RemoveAll(stagedDir); err != nil {
		return "", fmt.Errorf("remove stale function staging directory: %w", err)
	}

	if err := copyDir(pkg.dir, stagedDir); err != nil {
		return "", fmt.Errorf("stage function %s: %w", pkg.name, err)
	}

	inputs := function.With
	if inputs == nil {
		inputs = map[string]any{}
	}

	inputFile, err := json.MarshalIndent(inputs, "", "  ")
	if err != nil {
		return "", fmt.Errorf("encode function inputs: %w", err)
	}

	if err := os.WriteFile(filepath.Join(stagedDir, functionInputsFile), inputFile, 0o600); err != nil {
		return "", fmt.Errorf("write function inputs: %w", err)
	}

	worker, err := functionWorkerSource(vm, pkg, function)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(filepath.Join(stagedDir, functionWorkerFile), []byte(worker), 0o600); err != nil {
		return "", fmt.Errorf("write function worker: %w", err)
	}

	return stagedDir, nil
}

func functionWorkerSource(vm VM, pkg functionPackage, function templateFunction) (string, error) {
	if vm.HostIP == "" {
		return "", errors.New("host ip is required")
	}

	handlerPath := path.Clean(strings.TrimSpace(filepath.ToSlash(pkg.manifest.Handler)))
	if err := validateFunctionHandlerPath(pkg.name, handlerPath); err != nil {
		return "", err
	}

	queuePath, err := functionQueuePath(function.Trigger)
	if err != nil {
		return "", err
	}

	queueURL := "http://" + net.JoinHostPort(vm.HostIP, strconv.Itoa(queueProxyPort())) + queuePath
	workerID := vm.EnvironmentID + ":" + pkg.name

	return fmt.Sprintf(`import handler from %q;
import inputs from "./%s";

const queueURL = %q;
const workerID = %q;
const leaseMS = 300_000;
const idleDelayMS = 1_000;
const errorDelayMS = 2_000;

function sleep(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function errorMessage(error: unknown) {
  if (error instanceof Error) return error.stack || error.message;
  return String(error);
}

async function postTask(taskID: string, action: "ack" | "fail", body: unknown) {
  const response = await fetch(queueURL + "/tasks/" + encodeURIComponent(taskID) + "/" + action, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });

  if (!response.ok) {
    throw new Error(action + " failed with " + response.status + ": " + await response.text());
  }
}

while (true) {
  try {
    const lease = await fetch(queueURL + "/lease", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ worker_id: workerID, lease_ms: leaseMS }),
    });

    if (lease.status === 204) {
      await sleep(idleDelayMS);
      continue;
    }

    if (!lease.ok) {
      throw new Error("lease failed with " + lease.status + ": " + await lease.text());
    }

    const task = await lease.json();
    try {
      const workerData = await handler({ inputs, data: task.data });
      await postTask(task.id, "ack", { worker_id: workerID, worker_data: workerData ?? null });
    } catch (error) {
      await postTask(task.id, "fail", { worker_id: workerID, error: errorMessage(error) });
    }
  } catch (error) {
    console.error(errorMessage(error));
    await sleep(errorDelayMS);
  }
}
`, "./"+handlerPath, functionInputsFile, queueURL, workerID), nil
}

func functionQueuePath(trigger templateFunctionTrigger) (string, error) {
	if trigger.Type != "queue" {
		return "", fmt.Errorf("unsupported function trigger type %q", trigger.Type)
	}

	switch {
	case trigger.ID != "" && trigger.Key == "":
		return "/v1/queues/" + url.PathEscape(trigger.ID), nil
	case trigger.ID == "" && trigger.Key != "":
		return "/v1/queues/by-key/" + url.PathEscape(trigger.Key), nil
	default:
		return "", errors.New("function queue trigger must define exactly one of id or key")
	}
}

func (m Manager) copyFunctionToGuest(ctx context.Context, vm VM, srcDir, guestParent string, logs io.Writer) error {
	if err := m.runGuestCommand(ctx, vm, "mkdir -p "+shellQuote(guestParent), logs); err != nil {
		return fmt.Errorf("prepare function guest directory: %w", err)
	}

	args, err := scpGuestArgs(vm, srcDir, guestParent)
	if err != nil {
		return err
	}

	if err := m.run(ctx, "scp", args...); err != nil {
		return fmt.Errorf("copy function to guest: %w", sanitizeGuestCommandError(err))
	}

	return nil
}

func functionWorkerCommand(guestDir, name string) string {
	logPath := path.Join("/var/log/bastion/functions", name+".log")

	return strings.Join([]string{
		"set -eu",
		"mkdir -p /var/log/bastion/functions",
		"cd " + shellQuote(guestDir),
		"nohup bun ./" + functionWorkerFile + " > " + shellQuote(logPath) + " 2>&1 &",
	}, "\n")
}

func queueProxyPort() int {
	value := config.EnvDefault("QUEUE_PROXY_PORT", config.DefaultQueueProxyPort)

	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		return 3150
	}

	return port
}
