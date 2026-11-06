// Package postgres opens the enterprise storage backend.
package postgres

import (
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func Open(dsn string, maxConnections int) (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		// Driver errors may contain the DSN and credentials.
		return nil, errors.New("invalid PostgreSQL connection settings")
	}
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(maxConnections)
	db.SetMaxIdleConns(maxConnections)
	db.SetConnMaxLifetime(30 * time.Minute)
	return db, nil
}
