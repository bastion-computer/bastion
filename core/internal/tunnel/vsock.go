package tunnel

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const connectAckTimeout = 5 * time.Second

// DialGuestProxy opens a host-initiated Cloud Hypervisor vsock connection to the guest proxy.
func DialGuestProxy(ctx context.Context, vsockSocketPath string) (net.Conn, error) {
	return DialVsockPort(ctx, vsockSocketPath, GuestProxyVsockPort)
}

// DialVsockPort opens a host-initiated Cloud Hypervisor vsock connection to a guest port.
func DialVsockPort(ctx context.Context, vsockSocketPath string, port int) (net.Conn, error) {
	if strings.TrimSpace(vsockSocketPath) == "" {
		return nil, errors.New("vsock socket path is required")
	}

	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid vsock port %d", port)
	}

	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", vsockSocketPath)
	if err != nil {
		return nil, err
	}

	reader, err := connectVsockPort(conn, port)
	if err != nil {
		_ = conn.Close()

		return nil, err
	}

	if reader.Buffered() > 0 {
		return bufferedConn{Conn: conn, reader: reader}, nil
	}

	return conn, nil
}

func connectVsockPort(conn net.Conn, port int) (*bufio.Reader, error) {
	if err := conn.SetDeadline(time.Now().Add(connectAckTimeout)); err != nil {
		return nil, err
	}

	if _, err := fmt.Fprintf(conn, "CONNECT %d\n", port); err != nil {
		return nil, err
	}

	reader := bufio.NewReader(conn)

	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("read vsock connect ack: %w", err)
	}

	if err := validateConnectAck(line); err != nil {
		return nil, err
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		return nil, err
	}

	return reader, nil
}

func validateConnectAck(line string) error {
	fields := strings.Fields(line)
	if len(fields) != 2 || fields[0] != "OK" {
		return fmt.Errorf("unexpected vsock connect ack %q", strings.TrimSpace(line))
	}

	if _, err := strconv.Atoi(fields[1]); err != nil {
		return fmt.Errorf("unexpected vsock connect ack %q", strings.TrimSpace(line))
	}

	return nil
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c bufferedConn) Read(p []byte) (int, error) {
	if c.reader.Buffered() > 0 {
		return c.reader.Read(p)
	}

	return c.Conn.Read(p)
}
