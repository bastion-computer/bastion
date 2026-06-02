package cloudhypervisor

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultVMNetworkPrefix = "10.241"
	vmNetworkPrefixEnv     = "BASTION_VM_NETWORK_PREFIX"
)

type vmNetworkPrefix struct {
	value  string
	first  int
	second int
}

type networkPlan struct {
	tapName      string
	networkIndex int
	hostIP       string
	hostCIDR     string
	guestIP      string
	guestCIDR    string
	guestMAC     string
	networkCIDR  string
	hostIface    string
}

func planNetwork(environmentID string, networkIndex int) (networkPlan, error) {
	prefix, err := parseVMNetworkPrefix(os.Getenv(vmNetworkPrefixEnv))
	if err != nil {
		return networkPlan{}, err
	}

	return planNetworkWithPrefix(environmentID, networkIndex, prefix)
}

func planNetworkWithPrefix(environmentID string, networkIndex int, prefix vmNetworkPrefix) (networkPlan, error) {
	if networkIndex < 0 || networkIndex >= NetworkIndexLimit {
		return networkPlan{}, fmt.Errorf("network index %d out of range", networkIndex)
	}

	hash := fnv.New32a()
	_, _ = hash.Write([]byte(environmentID))

	encoded := make([]byte, 4)
	binary.BigEndian.PutUint32(encoded, hash.Sum32())
	tapName := "bt" + hex.EncodeToString(encoded)

	third := networkIndex / 64
	base := (networkIndex % 64) * 4
	hostIP := fmt.Sprintf("%s.%d.%d", prefix.value, third, base+1)
	guestIP := fmt.Sprintf("%s.%d.%d", prefix.value, third, base+2)

	return networkPlan{
		tapName:      tapName,
		networkIndex: networkIndex,
		hostIP:       hostIP,
		hostCIDR:     hostIP + "/30",
		guestIP:      guestIP,
		guestCIDR:    guestIP + "/30",
		guestMAC:     fmt.Sprintf("06:00:%02X:%02X:%02X:%02X", prefix.first, prefix.second, third, base+2),
		networkCIDR:  fmt.Sprintf("%s.%d.%d/30", prefix.value, third, base),
	}, nil
}

func parseVMNetworkPrefix(value string) (vmNetworkPrefix, error) {
	if strings.TrimSpace(value) == "" {
		value = defaultVMNetworkPrefix
	}

	firstText, secondText, ok := strings.Cut(value, ".")
	if !ok || strings.Contains(secondText, ".") {
		return vmNetworkPrefix{}, fmt.Errorf("%s must be an IPv4 /16 prefix like 10.241", vmNetworkPrefixEnv)
	}

	first, err := parseIPv4Octet(firstText)
	if err != nil {
		return vmNetworkPrefix{}, fmt.Errorf("%s first octet: %w", vmNetworkPrefixEnv, err)
	}

	second, err := parseIPv4Octet(secondText)
	if err != nil {
		return vmNetworkPrefix{}, fmt.Errorf("%s second octet: %w", vmNetworkPrefixEnv, err)
	}

	return vmNetworkPrefix{value: fmt.Sprintf("%d.%d", first, second), first: first, second: second}, nil
}

func parseIPv4Octet(value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}

	if parsed < 0 || parsed > 255 {
		return 0, fmt.Errorf("%d is out of range", parsed)
	}

	return parsed, nil
}

func (m Manager) setupTap(ctx context.Context, plan networkPlan) (networkPlan, error) {
	hostIface, err := m.defaultRouteInterface(ctx)
	if err != nil {
		return plan, err
	}

	plan.hostIface = hostIface

	if err := m.cleanupStaleTapCIDR(ctx, plan); err != nil {
		return plan, err
	}

	_ = m.run(ctx, "ip", "link", "del", plan.tapName)
	if err := m.ensureNetworkCIDRAvailable(ctx, plan); err != nil {
		return plan, err
	}

	if err := m.run(ctx, "ip", "tuntap", "add", "dev", plan.tapName, "mode", "tap", "user", strconv.Itoa(m.UID), "group", strconv.Itoa(m.GID)); err != nil {
		return plan, err
	}

	if err := m.run(ctx, "ip", "addr", "add", plan.hostCIDR, "dev", plan.tapName); err != nil {
		_ = m.cleanupTap(ctx, plan)

		return plan, err
	}

	if err := m.run(ctx, "ip", "link", "set", "dev", plan.tapName, "up"); err != nil {
		_ = m.cleanupTap(ctx, plan)

		return plan, err
	}

	if err := m.run(ctx, "sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		_ = m.cleanupTap(ctx, plan)

		return plan, err
	}

	if err := m.ensureIPTables(ctx, plan); err != nil {
		_ = m.cleanupTap(ctx, plan)

		return plan, err
	}

	return plan, nil
}

func (m Manager) cleanupTap(ctx context.Context, plan networkPlan) error {
	if plan.tapName == "" {
		return nil
	}

	m.removeIPTables(ctx, plan)

	return m.run(ctx, "ip", "link", "del", plan.tapName)
}

func (m Manager) ensureIPTables(ctx context.Context, plan networkPlan) error {
	rules := [][]string{
		{"FORWARD", "-i", plan.tapName, "-j", "ACCEPT"},
		{"FORWARD", "-o", plan.tapName, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"},
	}

	for _, rule := range rules {
		if err := m.ensureIPTableRule(ctx, "", rule...); err != nil {
			return err
		}
	}

	return m.ensureIPTableRule(ctx, "nat", "POSTROUTING", "-s", plan.networkCIDR, "-o", plan.hostIface, "-j", "MASQUERADE")
}

func (m Manager) cleanupStaleTapCIDR(ctx context.Context, plan networkPlan) error {
	interfaces, err := m.tapInterfacesForCIDR(ctx, plan.hostCIDR)
	if err != nil {
		return err
	}

	for _, iface := range interfaces {
		if iface == plan.tapName {
			continue
		}

		m.removeIPTables(ctx, networkPlan{tapName: iface, networkCIDR: plan.networkCIDR, hostIface: plan.hostIface})
		_ = m.run(ctx, "ip", "link", "del", iface)
	}

	return nil
}

func (m Manager) ensureNetworkCIDRAvailable(ctx context.Context, plan networkPlan) error {
	output, err := m.output(ctx, "ip", "-o", "-4", "route", "show")
	if err != nil {
		return err
	}

	return validateNetworkCIDRAvailable(plan, output)
}

func validateNetworkCIDRAvailable(plan networkPlan, routes string) error {
	_, planned, err := net.ParseCIDR(plan.networkCIDR)
	if err != nil {
		return fmt.Errorf("parse planned VM network CIDR: %w", err)
	}

	for line := range strings.SplitSeq(routes, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == "default" {
			continue
		}

		route, err := parseRouteDestination(fields[0])
		if err != nil {
			continue
		}

		if cidrsOverlap(planned, route) {
			iface := routeInterface(fields)
			if iface != "" {
				iface = " on " + iface
			}

			return fmt.Errorf("VM network %s overlaps existing route %s%s; set %s to a different /16 prefix", plan.networkCIDR, fields[0], iface, vmNetworkPrefixEnv)
		}
	}

	return nil
}

func parseRouteDestination(value string) (*net.IPNet, error) {
	if strings.Contains(value, "/") {
		_, network, err := net.ParseCIDR(value)

		return network, err
	}

	ip := net.ParseIP(value)
	if ip == nil || ip.To4() == nil {
		return nil, fmt.Errorf("invalid IPv4 route destination %q", value)
	}

	return &net.IPNet{IP: ip.To4(), Mask: net.CIDRMask(32, 32)}, nil
}

func cidrsOverlap(left, right *net.IPNet) bool {
	return left.Contains(right.IP) || right.Contains(left.IP)
}

func routeInterface(fields []string) string {
	for i, field := range fields {
		if field == "dev" && i+1 < len(fields) {
			return fields[i+1]
		}
	}

	return ""
}

func (m Manager) tapInterfacesForCIDR(ctx context.Context, cidr string) ([]string, error) {
	output, err := m.output(ctx, "ip", "-o", "-4", "addr", "show")
	if err != nil {
		return nil, err
	}

	var interfaces []string

	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[2] != "inet" || fields[3] != cidr {
			continue
		}

		iface := strings.Split(fields[1], "@")[0]
		if strings.HasPrefix(iface, "bt") {
			interfaces = append(interfaces, iface)
		}
	}

	return interfaces, nil
}

func (m Manager) removeIPTables(ctx context.Context, plan networkPlan) {
	if plan.tapName == "" {
		return
	}

	_ = m.deleteIPTableRule(ctx, "", "FORWARD", "-i", plan.tapName, "-j", "ACCEPT")

	_ = m.deleteIPTableRule(ctx, "", "FORWARD", "-o", plan.tapName, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT")

	if plan.hostIface != "" && plan.networkCIDR != "" {
		_ = m.deleteIPTableRule(ctx, "nat", "POSTROUTING", "-s", plan.networkCIDR, "-o", plan.hostIface, "-j", "MASQUERADE")
	}
}

func (m Manager) ensureIPTableRule(ctx context.Context, table string, rule ...string) error {
	if err := m.run(ctx, "iptables", iptablesArgs(table, "-C", rule)...); err == nil {
		return nil
	}

	return m.run(ctx, "iptables", iptablesArgs(table, "-I", rule)...)
}

func (m Manager) deleteIPTableRule(ctx context.Context, table string, rule ...string) error {
	return m.run(ctx, "iptables", iptablesArgs(table, "-D", rule)...)
}

func iptablesArgs(table, action string, rule []string) []string {
	args := make([]string, 0, len(rule)+3)
	if table != "" {
		args = append(args, "-t", table)
	}

	args = append(args, action)
	args = append(args, rule...)

	return args
}

func (m Manager) defaultRouteInterface(ctx context.Context) (string, error) {
	output, err := m.output(ctx, "ip", "route", "show", "default")
	if err != nil {
		return "", err
	}

	fields := strings.Fields(output)
	for i, field := range fields {
		if field == "dev" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}

	return "", errors.New("default route interface not found")
}

func waitForTCP(ctx context.Context, host string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	address := net.JoinHostPort(host, strconv.Itoa(port))

	for time.Now().Before(deadline) {
		conn, err := (&net.Dialer{Timeout: time.Second}).DialContext(ctx, "tcp", address)
		if err == nil {
			_ = conn.Close()

			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}

	return fmt.Errorf("timed out waiting for SSH on %s", address)
}

func commandString(name string, args []string) string {
	return name + " " + strings.Join(args, " ")
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // bastiond intentionally runs selected host networking commands.
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %w: %s", commandString(name, args), err, strings.TrimSpace(string(output)))
	}

	return nil
}

func runCommandStream(ctx context.Context, logs io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // bastiond intentionally runs selected host commands.

	var output bytes.Buffer

	combined := &lockedWriter{writers: []io.Writer{&output}}
	if logs != nil {
		combined.writers = append(combined.writers, logs)
	}

	cmd.Stdout = combined
	cmd.Stderr = combined

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w: %s", commandString(name, args), err, strings.TrimSpace(output.String()))
	}

	return nil
}

type lockedWriter struct {
	mu      sync.Mutex
	writers []io.Writer
}

func (w *lockedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, writer := range w.writers {
		if _, err := writer.Write(p); err != nil {
			return 0, err
		}
	}

	return len(p), nil
}

func outputCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // bastiond intentionally runs selected host networking commands.

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s failed: %w", commandString(name, args), err)
	}

	return string(output), nil
}
