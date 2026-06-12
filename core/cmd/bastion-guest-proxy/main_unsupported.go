//go:build !linux

package main

import (
	"fmt"
	"os"
)

func main() {
	_, _ = fmt.Fprintln(os.Stderr, "bastion-guest-proxy is supported on Linux only")
	os.Exit(1)
}
