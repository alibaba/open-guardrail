// Package sqlite opens a private, persistent local database without CGO.
package sqlite

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"

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
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(path)}
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
