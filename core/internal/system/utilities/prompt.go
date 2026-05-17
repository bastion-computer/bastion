package utilities

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Prompt asks users to install missing utilities.
type Prompt struct {
	Yes bool
	In  io.Reader
	Out io.Writer
}

// ConfirmInstall returns whether missing utilities should be installed.
func (p Prompt) ConfirmInstall(missing []Utility) (bool, error) {
	if p.Yes {
		return true, nil
	}

	in := p.In
	if in == nil {
		in = os.Stdin
	}

	out := p.Out
	if out == nil {
		out = io.Discard
	}

	if _, err := fmt.Fprintf(out, "missing utilities: %s\ninstall missing utilities? [y/N] ", strings.Join(names(missing), ", ")); err != nil {
		return false, err
	}

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}

	switch answer := strings.ToLower(strings.TrimSpace(line)); answer {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
