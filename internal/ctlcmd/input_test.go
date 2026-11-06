package ctlcmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadInputFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "input.json")
	if err := os.WriteFile(path, []byte(`{"ok":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := readInputFile(path, 11)
	if err != nil || string(data) != `{"ok":true}` {
		t.Fatalf("data=%q err=%v", data, err)
	}
	for _, path := range []string{path, dir, filepath.Join(dir, "missing")} {
		if _, err := readInputFile(path, 10); err == nil {
			t.Fatalf("accepted invalid or oversized input %q", path)
		}
	}
}

func TestReadLimitedInputStopsAtBoundary(t *testing.T) {
	// A growing file/stream must not be read in full, even if metadata was small.
	source := bytes.NewBufferString(strings.Repeat("x", 100))
	if _, err := readLimitedInput(source, 10); err == nil {
		t.Fatal("accepted oversized input")
	}
	if source.Len() != 89 {
		t.Fatalf("read past limit+1: %d bytes remain", source.Len())
	}
	want := errors.New("read failed")
	if _, err := readLimitedInput(failedInput{want}, 10); !errors.Is(err, want) {
		t.Fatalf("lost read error: %v", err)
	}
}

type failedInput struct{ err error }

func (r failedInput) Read([]byte) (int, error) { return 0, r.err }
