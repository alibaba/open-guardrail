// Package sqlite opens a private, persistent local database without CGO.
package sqlite

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func Open(dir string) (*sql.DB, error) {
	return open(dir, true)
}

// OpenExisting never creates a directory or database, even if the file disappears.
func OpenExisting(dir string) (*sql.DB, error) { return open(dir, false) }

func open(dir string, create bool) (*sql.DB, error) {
	if create {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, errors.New("cannot create SQLite data directory")
		}
	}
	path := filepath.Join(dir, "guardrail.db")
	flags := os.O_RDWR
	if create {
		flags |= os.O_CREATE
	}
	f, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return nil, errors.New("cannot open SQLite database file")
	}
	if err = f.Close(); err != nil {
		return nil, errors.New("cannot close SQLite database file")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.New("cannot resolve SQLite database path")
	}
	// url.URL writes "//" after the scheme whenever Path is non-empty, so a
	// path without a leading slash ("data/guardrail.db", or "C:/..." from
	// filepath.Abs on Windows) would leave its first segment in the authority,
	// which SQLite rejects as "invalid uri authority". A slash-rooted absolute
	// path keeps the authority empty.
	slash := filepath.ToSlash(abs)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	u := url.URL{Scheme: "file", Path: slash}
	q := u.Query()
	if !create {
		q.Set("mode", "rw")
	}
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, errors.New("cannot initialize SQLite driver")
	}
	// One connection avoids competing writers inside this single-instance service.
	// Transactions and constraints, not this limit, enforce business invariants.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}
