// Package utilization reports host capacity and live environment resource usage.
package utilization

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	ch "github.com/bastion-computer/bastion/core/internal/cloudhypervisor"
	"github.com/bastion-computer/bastion/core/internal/database"
)

const (
	cpuTopologyPath = "/sys/devices/system/cpu"
	meminfoPath     = "/proc/meminfo"
	maxInt64        = int64(1<<63 - 1)
)

// Resource describes one host capacity dimension.
type Resource struct {
	Total     int64 `json:"total"`
	Used      int64 `json:"used"`
	Available int64 `json:"available"`
}

// Utilization describes point-in-time host capacity accounting.
type Utilization struct {
	VCPU   Resource `json:"vcpu"`
	Memory Resource `json:"memory"`
	Volume Resource `json:"volume"`
}

// HostCapacity contains raw host capacity values in API accounting units.
type HostCapacity struct {
	VCPU        int64
	MemoryBytes int64
	VolumeBytes int64
}

// HostCapacityProvider returns current host capacity.
type HostCapacityProvider func(context.Context) (HostCapacity, error)

// Option configures the utilization service.
type Option func(*Service)

// Service reports host capacity and current environment usage.
type Service struct {
	db           *database.Client
	hostCapacity HostCapacityProvider
}

// NewService returns a utilization service backed by db.
func NewService(db *database.Client, opts ...Option) *Service {
	service := &Service{db: db, hostCapacity: hostCapacityProvider(".")}
	for _, opt := range opts {
		opt(service)
	}

	if service.hostCapacity == nil {
		service.hostCapacity = hostCapacityProvider(".")
	}

	return service
}

// WithDataDir configures the data directory filesystem used for volume capacity.
func WithDataDir(dataDir string) Option {
	return func(s *Service) {
		s.hostCapacity = hostCapacityProvider(dataDir)
	}
}

// WithHostCapacityProvider configures host capacity detection.
func WithHostCapacityProvider(provider HostCapacityProvider) Option {
	return func(s *Service) {
		s.hostCapacity = provider
	}
}

// Get returns current host utilization.
func (s *Service) Get(ctx context.Context) (Utilization, error) {
	total, err := s.hostCapacity(ctx)
	if err != nil {
		return Utilization{}, err
	}

	used, err := s.usedCapacity(ctx)
	if err != nil {
		return Utilization{}, err
	}

	return Utilization{
		VCPU:   resource(total.VCPU, used.VCPU),
		Memory: resource(total.MemoryBytes, used.MemoryBytes),
		Volume: resource(total.VolumeBytes, used.VolumeBytes),
	}, nil
}

func (s *Service) usedCapacity(ctx context.Context) (HostCapacity, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT e.id, t.config
FROM environments e
JOIN templates t ON t.id = e.template_id
LEFT JOIN environment_vms v ON v.environment_id = e.id
WHERE e.status IN (?, ?, ?) OR v.state IN (?, ?, ?)
`, ch.StateCreating, ch.StateRunning, ch.StatePaused, ch.StateCreating, ch.StateRunning, ch.StatePaused)
	if err != nil {
		return HostCapacity{}, fmt.Errorf("query live environment resource usage: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var used HostCapacity

	for rows.Next() {
		var environmentID, config string
		if err := rows.Scan(&environmentID, &config); err != nil {
			return HostCapacity{}, fmt.Errorf("scan live environment resource usage: %w", err)
		}

		usage, err := ch.ResolveTemplateResourceUsage(json.RawMessage(config))
		if err != nil {
			return HostCapacity{}, fmt.Errorf("resolve resource usage for environment %s: %w", environmentID, err)
		}

		used.VCPU += usage.VCPU
		used.MemoryBytes += usage.MemoryBytes
		used.VolumeBytes += usage.VolumeBytes
	}

	if err := rows.Err(); err != nil {
		return HostCapacity{}, fmt.Errorf("iterate live environment resource usage: %w", err)
	}

	return used, nil
}

func resource(total, used int64) Resource {
	available := max(total-used, int64(0))
	return Resource{Total: total, Used: used, Available: available}
}

func hostCapacityProvider(dataDir string) HostCapacityProvider {
	if dataDir == "" {
		dataDir = "."
	}

	return func(context.Context) (HostCapacity, error) {
		return detectHostCapacity(dataDir)
	}
}

func detectHostCapacity(dataDir string) (HostCapacity, error) {
	vcpu, err := detectVCPUThreads(cpuTopologyPath)
	if err != nil || vcpu <= 0 {
		vcpu = int64(runtime.NumCPU())
	}

	memoryBytes, err := detectMemoryBytes(meminfoPath)
	if err != nil {
		return HostCapacity{}, err
	}

	volumeBytes, err := detectVolumeBytes(dataDir)
	if err != nil {
		return HostCapacity{}, err
	}

	return HostCapacity{VCPU: vcpu, MemoryBytes: memoryBytes, VolumeBytes: volumeBytes}, nil
}

func detectVCPUThreads(sysCPUPath string) (int64, error) {
	entries, err := os.ReadDir(sysCPUPath)
	if err != nil {
		return 0, fmt.Errorf("read CPU topology: %w", err)
	}

	online, _ := readOnlineCPUs(filepath.Join(sysCPUPath, "online"))
	packages := collectCPUTopology(sysCPUPath, entries, online)

	return cpuTopologyThreadCount(packages)
}

func collectCPUTopology(sysCPUPath string, entries []os.DirEntry, online map[int]bool) map[int]map[int]int {
	packages := make(map[int]map[int]int)

	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "cpu") {
			continue
		}

		cpuID, ok := parseCPUName(entry.Name())
		if !ok {
			continue
		}

		if online != nil && !online[cpuID] {
			continue
		}

		topologyDir := filepath.Join(sysCPUPath, entry.Name(), "topology")

		packageID, err := readIntFile(filepath.Join(topologyDir, "physical_package_id"))
		if err != nil {
			continue
		}

		coreID, err := readIntFile(filepath.Join(topologyDir, "core_id"))
		if err != nil {
			continue
		}

		cores := packages[packageID]
		if cores == nil {
			cores = make(map[int]int)
			packages[packageID] = cores
		}

		cores[coreID]++
	}

	return packages
}

func cpuTopologyThreadCount(packages map[int]map[int]int) (int64, error) {
	if len(packages) == 0 {
		return 0, errors.New("CPU topology is unavailable")
	}

	coresPerCPU := 0
	threadsPerCore := 0

	for _, cores := range packages {
		coresPerCPU = max(coresPerCPU, len(cores))

		for _, threads := range cores {
			threadsPerCore = max(threadsPerCore, threads)
		}
	}

	if coresPerCPU == 0 || threadsPerCore == 0 {
		return 0, errors.New("CPU topology is incomplete")
	}

	return int64(len(packages) * coresPerCPU * threadsPerCore), nil
}

func parseCPUName(name string) (int, bool) {
	value := strings.TrimPrefix(name, "cpu")
	if value == "" {
		return 0, false
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, false
	}

	return parsed, true
}

func readOnlineCPUs(path string) (map[int]bool, error) {
	contents, err := os.ReadFile(path) //nolint:gosec // Host CPU online path is fixed by internal capacity detection.
	if err != nil {
		return nil, err
	}

	return parseCPUList(strings.TrimSpace(string(contents)))
}

func parseCPUList(value string) (map[int]bool, error) {
	if value == "" {
		return nil, errors.New("CPU list is empty")
	}

	out := make(map[int]bool)

	for part := range strings.SplitSeq(value, ",") {
		startText, endText, hasRange := strings.Cut(part, "-")

		start, err := strconv.Atoi(startText)
		if err != nil {
			return nil, err
		}

		end := start
		if hasRange {
			end, err = strconv.Atoi(endText)
			if err != nil {
				return nil, err
			}
		}

		if end < start {
			return nil, fmt.Errorf("invalid CPU range %q", part)
		}

		for cpu := start; cpu <= end; cpu++ {
			out[cpu] = true
		}
	}

	return out, nil
}

func readIntFile(path string) (int, error) {
	contents, err := os.ReadFile(path) //nolint:gosec // Host CPU topology paths are fixed by internal capacity detection.
	if err != nil {
		return 0, err
	}

	return strconv.Atoi(strings.TrimSpace(string(contents)))
}

func detectMemoryBytes(path string) (int64, error) {
	contents, err := os.ReadFile(path) //nolint:gosec // Host meminfo path is fixed by internal capacity detection.
	if err != nil {
		return 0, fmt.Errorf("read host memory info: %w", err)
	}

	return parseMemTotalBytes(contents)
}

func parseMemTotalBytes(contents []byte) (int64, error) {
	for line := range strings.SplitSeq(string(contents), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.TrimSuffix(fields[0], ":") != "MemTotal" {
			continue
		}

		kib, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse MemTotal: %w", err)
		}

		if kib > maxInt64/1024 {
			return 0, errors.New("MemTotal is too large")
		}

		return kib * 1024, nil
	}

	return 0, errors.New("MemTotal not found")
}

func detectVolumeBytes(path string) (int64, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("stat data directory filesystem: %w", err)
	}

	if stat.Bsize <= 0 || stat.Blocks == 0 {
		return 0, nil
	}

	blockSize := uint64(stat.Bsize)
	if stat.Blocks > uint64(maxInt64)/blockSize {
		return maxInt64, nil
	}

	return int64(stat.Blocks * blockSize), nil //nolint:gosec // Overflow is checked above before converting to int64.
}
