package storage

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alibaba/open-guardrail/internal/application/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// Every test gets its own schema, including tests that deliberately corrupt
// migration metadata. Reopening uses this same DSN; later tests use a fresh one.
func isolatedPostgresDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; use a disposable PostgreSQL database")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test DSN")
	}
	admin := stdlib.OpenDB(*cfg)
	t.Cleanup(func() { admin.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	schema := "guardrail_test_" + strings.ToLower(identity.RandomToken("")[:24])
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(cleanup, "DROP SCHEMA "+quoted+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		address, err := url.Parse(dsn)
		if err != nil {
			t.Fatal("invalid test DSN")
		}
		query := address.Query()
		query.Set("search_path", schema)
		address.RawQuery = query.Encode()
		return address.String()
	}
	// pgx also accepts PostgreSQL keyword/value DSNs. The generated schema name
	// contains only lowercase ASCII letters, digits and underscores.
	return dsn + " search_path=" + schema
}
