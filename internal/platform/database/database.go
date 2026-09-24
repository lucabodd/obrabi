// Package database opens PostgreSQL pools and applies each service's
// embedded SQL migrations.
package database

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens a pool and waits (up to timeout) for the database to accept
// connections, which smooths over container start ordering.
func Connect(ctx context.Context, url string, timeout time.Duration, log *slog.Logger) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for attempt := 1; ; attempt++ {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = pool.Ping(pingCtx)
		cancel()
		if err == nil {
			return pool, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			pool.Close()
			return nil, fmt.Errorf("database not reachable after %d attempts: %w", attempt, err)
		}
		log.Warn("waiting for database", "attempt", attempt, "err", err)
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

var schemaName = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Migrate applies, in lexical order, every *.sql file of fsys that has not been
// applied yet to the given schema. Each file runs in its own transaction and
// is recorded in <schema>.schema_migrations. An advisory lock serialises
// concurrent replicas of the same service.
func Migrate(ctx context.Context, pool *pgxpool.Pool, schema string, fsys fs.FS, log *slog.Logger) error {
	if !schemaName.MatchString(schema) {
		return fmt.Errorf("invalid schema name %q", schema)
	}
	ident := pgx.Identifier{schema}.Sanitize()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	lockKey := "obrabi.migrate." + schema
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock(hashtext($1))", lockKey); err != nil {
		return fmt.Errorf("take migration lock: %w", err)
	}
	defer func() {
		// Use a fresh context: the caller's may already be cancelled.
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock(hashtext($1))", lockKey)
	}()

	// CREATE SCHEMA IF NOT EXISTS still requires CREATE on the database, which
	// the per-service roles do not have (the schema is pre-created by the
	// postgres init script), so only create it when it is really missing.
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = $1)", schema).Scan(&exists); err != nil {
		return fmt.Errorf("check schema: %w", err)
	}
	if !exists {
		if _, err := conn.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
			return fmt.Errorf("create schema %s: %w", schema, err)
		}
	}

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS `+ident+`.schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied := map[string]bool{}
	rows, err := conn.Query(ctx, "SELECT version FROM "+ident+".schema_migrations")
	if err != nil {
		return fmt.Errorf("read applied migrations: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	files, err := fs.Glob(fsys, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, name := range files {
		version := strings.TrimSuffix(path.Base(name), ".sql")
		if applied[version] {
			continue
		}
		body, err := fs.ReadFile(fsys, name)
		if err != nil {
			return err
		}
		if err := applyOne(ctx, conn.Conn(), ident, version, string(body)); err != nil {
			return fmt.Errorf("migration %s/%s: %w", schema, name, err)
		}
		log.Info("migration applied", "schema", schema, "version", version)
	}
	return nil
}

func applyOne(ctx context.Context, conn *pgx.Conn, ident, version, body string) (err error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	// Without arguments pgx uses the simple protocol, so a file may contain
	// several statements.
	if _, err = tx.Exec(ctx, body); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO "+ident+".schema_migrations (version) VALUES ($1)", version); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// IsUniqueViolation reports whether err is a PostgreSQL unique_violation.
func IsUniqueViolation(err error) bool { return hasCode(err, "23505") }

// IsForeignKeyViolation reports whether err is a PostgreSQL foreign_key_violation.
func IsForeignKeyViolation(err error) bool { return hasCode(err, "23503") }

func hasCode(err error, code string) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == code
}
