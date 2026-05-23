package system

import (
	"fmt"
	"io"
	"strings"
	"time"
)

const cloudHypervisorDownloadBarWidth = 24

func logCloudHypervisorProgress(w io.Writer, format string, args ...any) error {
	if w == nil {
		return nil
	}

	_, err := fmt.Fprintf(w, "bastion: "+format+"\n", args...)

	return err
}

type cloudHypervisorDownloadProgress struct {
	out        io.Writer
	name       string
	total      int64
	downloaded int64
	started    bool
	lastRender time.Time
}

func newCloudHypervisorDownloadProgress(out io.Writer, name string, total int64) *cloudHypervisorDownloadProgress {
	return &cloudHypervisorDownloadProgress{out: out, name: name, total: total}
}

func (p *cloudHypervisorDownloadProgress) Write(contents []byte) (int, error) {
	p.downloaded += int64(len(contents))
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

func (p *cloudHypervisorDownloadProgress) finish(done bool) error {
	if p.out == nil {
		return nil
	}

	if err := p.render(done); err != nil {
		return err
	}

	_, err := fmt.Fprintln(p.out)

	return err
}

func (p *cloudHypervisorDownloadProgress) complete() bool {
	return p.total > 0 && p.downloaded >= p.total
}

func (p *cloudHypervisorDownloadProgress) render(done bool) error {
	p.started = true
	p.lastRender = time.Now()

	if p.total <= 0 {
		_, err := fmt.Fprintf(p.out, "\rbastion: %s downloaded %s", p.name, formatBytes(p.downloaded))

		return err
	}

	percent := float64(p.downloaded) / float64(p.total)
	if percent > 1 {
		percent = 1
	}

	if done {
		percent = 1
	}

	_, err := fmt.Fprintf(
		p.out,
		"\rbastion: %s [%s] %3.0f%% %s/%s",
		p.name,
		downloadBar(percent),
		percent*100,
		formatBytes(p.downloaded),
		formatBytes(p.total),
	)

	return err
}

func downloadBar(percent float64) string {
	filled := int(percent * cloudHypervisorDownloadBarWidth)
	filled = min(filled, cloudHypervisorDownloadBarWidth)

	if filled == cloudHypervisorDownloadBarWidth {
		return strings.Repeat("=", cloudHypervisorDownloadBarWidth)
	}

	return strings.Repeat("=", filled) + ">" + strings.Repeat(".", cloudHypervisorDownloadBarWidth-filled-1)
}

func formatBytes(size int64) string {
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
