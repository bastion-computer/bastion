package cloudhypervisor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriteVMStateConcurrentReadersNeverSeeMalformedJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	vm := VM{
		EnvironmentID: "env_atomic_state",
		VMID:          "vm-atomic-state",
		State:         StateRunning,
		EnvDir:        dir,
	}
	if err := writeVMState(vm); err != nil {
		t.Fatalf("write initial vm state: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for ctx.Err() == nil {
				if _, err := readVMState(dir); err != nil {
					select {
					case errCh <- err:
					default:
					}

					cancel()

					return
				}
			}
		})
	}

	for i := range 500 {
		vm.LastError = strings.Repeat(fmt.Sprintf("state-%03d", i), 64*1024)
		if i%2 == 0 {
			vm.LastError = ""
		}

		if err := writeVMState(vm); err != nil {
			cancel()
			wg.Wait()

			t.Fatalf("write vm state: %v", err)
		}
	}

	cancel()
	wg.Wait()

	select {
	case err := <-errCh:
		t.Fatalf("read vm state while writing: %v", err)
	default:
	}
}

func TestWriteVMStateRemovesTemporaryFileAfterRenameError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	stateFile := statePath(dir)
	if err := os.Mkdir(stateFile, 0o700); err != nil {
		t.Fatalf("create state path directory: %v", err)
	}

	err := writeVMState(VM{EnvironmentID: "env_temp_cleanup", EnvDir: dir})
	if err == nil {
		t.Fatal("write vm state succeeded with directory at state path")
	}

	matches, globErr := filepath.Glob(filepath.Join(dir, ".vm.json.tmp-*"))
	if globErr != nil {
		t.Fatalf("glob temp files: %v", globErr)
	}

	if len(matches) != 0 {
		t.Fatalf("temporary files left after failed write: %v", matches)
	}

	if _, statErr := os.Stat(stateFile); statErr != nil {
		t.Fatalf("state path directory missing after failed write: %v", statErr)
	}
}
