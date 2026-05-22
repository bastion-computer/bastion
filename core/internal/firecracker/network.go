package firecracker

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const bastionGuestCIDR = "10.241.%d.%d/30"

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
	hostIP := fmt.Sprintf("10.241.%d.%d", third, base+1)
	guestIP := fmt.Sprintf("10.241.%d.%d", third, base+2)

	return networkPlan{
		tapName:      tapName,
		networkIndex: networkIndex,
		hostIP:       hostIP,
		hostCIDR:     hostIP + "/30",
		guestIP:      guestIP,
		guestCIDR:    guestIP + "/30",
		guestMAC:     fmt.Sprintf("06:00:0A:F1:%02X:%02X", third, base+2),
		networkCIDR:  fmt.Sprintf(bastionGuestCIDR, third, base),
	}, nil
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

func ipNet(cidr string) (net.IPNet, error) {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return net.IPNet{}, err
	}

	network.IP = ip

	return *network, nil
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

func outputCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // bastiond intentionally runs selected host networking commands.

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s failed: %w", commandString(name, args), err)
	}

	return string(output), nil
}
