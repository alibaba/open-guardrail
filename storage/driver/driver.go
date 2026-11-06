// Package driver registers compiled-in database extensions. It does not load code
// dynamically. Register extensions before starting the service or opening storage.
package driver

import (
	"database/sql"
	"errors"
	"io/fs"
	"regexp"
	"sort"
	"sync"

	"github.com/pressly/goose/v3"
)

// Options contains connection settings. Implementations must not include the URL
// or credentials in returned errors.
type Options struct {
	DataDir        string
	URL            string
	MaxConnections int
}

// Definition supplies the database-specific parts of the shared SQL store.
// Migrations is rooted directly at the numbered SQL migration files.
// ConflictClause receives a trusted comma-separated column list and returns the
// suffix used by an idempotent INSERT. IsDuplicate handles a duplicate-key error
// when that INSERT is deliberately left unchanged.
type Definition struct {
	Name           string
	Dialect        goose.Dialect
	Migrations     fs.FS
	VersionTable   string
	Validate       func(Options) error
	Open           func(Options) (*sql.DB, error)
	OpenExisting   func(Options) (*sql.DB, error)
	ConflictClause func(columns string) string
	IsDuplicate    func(error) bool
}

var registry = struct {
	sync.RWMutex
	entries map[string]Definition
}{entries: make(map[string]Definition)}
var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Register adds an extension exactly once. Built-in drivers cannot be replaced.
func Register(d Definition) error {
	if !identifier.MatchString(d.Name) || d.Name == "sqlite" || d.Name == "postgres" ||
		!identifier.MatchString(d.VersionTable) || d.Dialect == "" || d.Migrations == nil ||
		d.Validate == nil || d.Open == nil || d.OpenExisting == nil ||
		d.ConflictClause == nil || d.IsDuplicate == nil {
		return errors.New("invalid database extension definition")
	}
	registry.Lock()
	defer registry.Unlock()
	if _, exists := registry.entries[d.Name]; exists {
		return errors.New("database extension already registered")
	}
	registry.entries[d.Name] = d
	return nil
}

// Definitions returns a stable copy; callers cannot mutate the registry map.
func Definitions() []Definition {
	registry.RLock()
	defer registry.RUnlock()
	result := make([]Definition, 0, len(registry.entries))
	for _, d := range registry.entries {
		result = append(result, d)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}
