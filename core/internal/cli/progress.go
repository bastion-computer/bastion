package cli

import (
	"fmt"
	"io"
	"strings"
	"time"
)

const archiveProgressBarWidth = 24

type archiveProgress struct {
	out         io.Writer
	label       string
	total       int64
	transferred int64
	started     bool
	lastRender  time.Time
	tick        int
}

func newArchiveProgress(out io.Writer, label string, total int64) *archiveProgress {
	return &archiveProgress{out: out, label: label, total: total}
}

func (p *archiveProgress) Write(contents []byte) (int, error) {
	p.transferred += int64(len(contents))
	if p.out == nil {
		return len(contents), nil
	}

	now := time.Now()
	if !p.started || now.Sub(p.lastRender) >= 200*time.Millisecond || p.complete() {
		if err := p.render(false); err != nil {
			return 0, err
		}
	}

	return len(contents), nil
}

func (p *archiveProgress) finish(done bool) error {
	if p.out == nil {
		return nil
	}

	if err := p.render(done); err != nil {
		return err
	}

	_, err := fmt.Fprintln(p.out)

	return err
}

func (p *archiveProgress) complete() bool {
	return p.total > 0 && p.transferred >= p.total
}

func (p *archiveProgress) render(done bool) error {
	p.started = true
	p.lastRender = time.Now()

	if p.total <= 0 {
		bar := archiveIndeterminateBar(p.tick, done)
		p.tick++

		_, err := fmt.Fprintf(p.out, "\rbastion: %s [%s] %s", p.label, bar, formatArchiveBytes(p.transferred))

		return err
	}

	percent := float64(p.transferred) / float64(p.total)
	if percent > 1 {
		percent = 1
	}

	if done {
		percent = 1
	}

	_, err := fmt.Fprintf(
		p.out,
		"\rbastion: %s [%s] %3.0f%% %s/%s",
		p.label,
		archiveProgressBar(percent),
		percent*100,
		formatArchiveBytes(p.transferred),
		formatArchiveBytes(p.total),
	)

	return err
}

func archiveProgressBar(percent float64) string {
	filled := int(percent * archiveProgressBarWidth)
	filled = min(filled, archiveProgressBarWidth)

	if filled == archiveProgressBarWidth {
		return strings.Repeat("=", archiveProgressBarWidth)
	}

	return strings.Repeat("=", filled) + ">" + strings.Repeat(".", archiveProgressBarWidth-filled-1)
}

func archiveIndeterminateBar(tick int, done bool) string {
	if done {
		return strings.Repeat("=", archiveProgressBarWidth)
	}

	const segment = 6

	start := tick%(archiveProgressBarWidth+segment) - segment

	var bar strings.Builder

	for i := range archiveProgressBarWidth {
		if i >= start && i < start+segment {
			bar.WriteByte('=')
		} else {
			bar.WriteByte('.')
		}
	}

	return bar.String()
}

func formatArchiveBytes(size int64) string {
	const unit = 1024

	if size < unit {
		return fmt.Sprintf("%d B", size)
	}

	value := float64(size)
	for _, suffix := range []string{"KiB", "MiB", "GiB", "TiB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}

	return fmt.Sprintf("%.1f PiB", value/unit)
}
