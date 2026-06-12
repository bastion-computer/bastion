// Package tunnel defines the internal guest tunnel proxy protocol.
package tunnel

const (
	// GuestProxyVsockPort is the fixed guest-side vsock port for HTTP tunnel proxying.
	GuestProxyVsockPort = 3148

	// TargetPortHeader tells the guest proxy which localhost port to dial.
	TargetPortHeader = "X-Bastion-Tunnel-Port"
)
