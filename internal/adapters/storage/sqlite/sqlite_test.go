package sqlite

import (
	"context"
	"testing"
)

// An absolute data directory is what t.TempDir and the documented data_dir
// values give. On Windows it has no leading slash ("C:/..."), which is also
// the shape a bare filepath.Abs result takes.
func TestOpenAbsoluteDataDir(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

// The default data directory is the relative "data", so the URI must keep an
// empty authority: a first segment read as a host fails to open.
func TestOpenRelativeDataDir(t *testing.T) {
	t.Chdir(t.TempDir())
	db, err := Open("data")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if _, err := db.ExecContext(context.Background(), "SELECT 1"); err != nil {
		t.Fatalf("query: %v", err)
	}
}
