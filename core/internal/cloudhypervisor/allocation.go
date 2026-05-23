package cloudhypervisor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const networkLockFileName = "network.lock"

type networkLock struct {
	file *os.File
}

func (m Manager) reserveNetwork(environmentID, dir string) (VM, error) {
	lock, err := m.lockNetwork()
	if err != nil {
		return VM{}, err
	}

	defer func() { _ = lock.Close() }()

	used, err := m.usedNetworkIndices()
	if err != nil {
		return VM{}, err
	}

	networkIndex, ok := firstFreeNetworkIndex(used)
	if !ok {
		return VM{}, errors.New("allocate environment network: no available network indices")
	}

	createdAt := now()
	vm := VM{
		EnvironmentID: environmentID,
		VMID:          shortID(environmentID),
		State:         StateCreating,
		EnvDir:        dir,
		NetworkIndex:  networkIndex,
		CreatedAt:     createdAt,
		UpdatedAt:     createdAt,
	}

	if err := writeVMState(vm); err != nil {
		return VM{}, fmt.Errorf("write initial vm state: %w", err)
	}

	return vm, nil
}

func (m Manager) lockNetwork() (*networkLock, error) {
	lockDir := filepath.Join(m.DataDir, assetDirName)
	if err := os.MkdirAll(lockDir, 0o750); err != nil {
		return nil, fmt.Errorf("create network lock directory: %w", err)
	}

	file, err := os.OpenFile(filepath.Join(lockDir, networkLockFileName), os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // Lock path is rooted in the configured Bastion data directory.
	if err != nil {
		return nil, fmt.Errorf("open network lock: %w", err)
	}

	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX); err != nil {
		_ = file.Close()

		return nil, fmt.Errorf("lock network allocation: %w", err)
	}

	return &networkLock{file: file}, nil
}

func (l *networkLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}

	unlockErr := unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
	closeErr := l.file.Close()

	if unlockErr != nil {
		return unlockErr
	}

	return closeErr
}

func (m Manager) usedNetworkIndices() (map[int]struct{}, error) {
	used := make(map[int]struct{})
	dir := filepath.Join(m.DataDir, environmentsDir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return used, nil
		}

		return nil, fmt.Errorf("read environment directories: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		vm, err := readVMState(filepath.Join(dir, entry.Name()))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}

			return nil, fmt.Errorf("read vm network allocation for %s: %w", entry.Name(), err)
		}

		if vm.NetworkIndex < 0 || vm.NetworkIndex >= NetworkIndexLimit {
			return nil, fmt.Errorf("vm %s has network index %d out of range", vm.EnvironmentID, vm.NetworkIndex)
		}

		used[vm.NetworkIndex] = struct{}{}
	}

	return used, nil
}

func firstFreeNetworkIndex(used map[int]struct{}) (int, bool) {
	for networkIndex := range NetworkIndexLimit {
		if _, ok := used[networkIndex]; !ok {
			return networkIndex, true
		}
	}

	return 0, false
}
