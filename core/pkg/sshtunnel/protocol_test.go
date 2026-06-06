package sshtunnel

import (
	"bytes"
	"encoding/binary"
	"strings"
	"sync"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	payload := []byte("hello")

	if err := WriteFrame(&buf, FrameStdout, payload); err != nil {
		t.Fatalf("write frame: %v", err)
	}

	frameType, gotPayload, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}

	if frameType != FrameStdout {
		t.Fatalf("frame type = %d, want %d", frameType, FrameStdout)
	}

	if !bytes.Equal(gotPayload, payload) {
		t.Fatalf("payload = %q, want %q", gotPayload, payload)
	}
}

func TestWriteFrameRejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	err := WriteFrame(&bytes.Buffer{}, FrameStdout, make([]byte, MaxPayload+1))
	if err == nil || !strings.Contains(err.Error(), "payload too large") {
		t.Fatalf("write oversized frame error = %v, want payload too large", err)
	}
}

func TestReadFrameRejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	var header [5]byte

	header[0] = FrameStdout
	binary.BigEndian.PutUint32(header[1:], MaxPayload+1)

	_, _, err := ReadFrame(bytes.NewReader(header[:]))
	if err == nil || !strings.Contains(err.Error(), "payload too large") {
		t.Fatalf("read oversized frame error = %v, want payload too large", err)
	}
}

func TestFrameWriterSerializesConcurrentWrites(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	writer := NewFrameWriter(&buf)
	values := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24}

	var wg sync.WaitGroup
	for _, value := range values {
		wg.Add(1)

		go func(index byte) {
			defer wg.Done()

			payload := []byte{index}
			if err := writer.WriteFrame(FrameStdout, payload); err != nil {
				t.Errorf("write frame %d: %v", index, err)
			}
		}(value)
	}

	wg.Wait()

	seen := make(map[byte]bool)

	for range 25 {
		frameType, payload, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}

		if frameType != FrameStdout {
			t.Fatalf("frame type = %d, want %d", frameType, FrameStdout)
		}

		if len(payload) != 1 {
			t.Fatalf("payload length = %d, want 1", len(payload))
		}

		seen[payload[0]] = true
	}

	for _, value := range values {
		if !seen[value] {
			t.Fatalf("missing payload %d", value)
		}
	}
}
