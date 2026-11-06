package servercmd

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestUsageAndInvalidCommands(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, args := range [][]string{nil, {"--help"}, {"version"}, {"serve", "--help"}, {"admin"}, {"admin", "--help"}, {"admin", "reset", "--help"}} {
		if err := run(context.Background(), args, io.Discard, logger); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"unknown"}, {"serve", "extra"}, {"serve", "--unknown"}, {"admin", "unknown"}, {"admin", "reset", "--password", "secret"}, {"admin", "reset", "extra"}} {
		if err := run(context.Background(), args, io.Discard, logger); err == nil {
			t.Fatal("expected error")
		}
	}
}
