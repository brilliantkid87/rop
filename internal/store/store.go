// Package store provides durable persistence for ROP Actions, Reversal
// Attempts, private reversal material, and verification results (SQLite for
// the reference MVP, Master Prompt §72). Actions are insert-only; status
// changes write an append-only history row so reversal never erases original
// history (invariant I-1).
//
// This package is ROP Core: it MUST NOT import any HTTP package
// (invariant I-17, enforced by a repository test).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DBTX is the common surface of *sql.DB and *sql.Tx, so provider adapters can
// run business writes and Action writes in one transaction (architecture §9).
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// ErrAttemptInProgress is returned when the one-non-concluded-attempt
// constraint rejects a second concurrent reversal attempt (architecture §10).
var ErrAttemptInProgress = errors.New("a non-concluded reversal attempt already exists for this action")

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the SQLite database at dbPath.
func Open(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("store: create db directory: %w", err)
	}
	// WAL + FULL synchronous: crash-consistency choices documented in
	// docs/failure-model.md §2; busy_timeout for single-process contention.
	dsn := "file:" + strings.ReplaceAll(dbPath, "\\", "/") +
		"?_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}
	// modernc/sqlite allows a single writer; keep pool small and deterministic.
	db.SetMaxOpenConns(1)
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// DB returns the raw handle for provider adapters that must write business
// state and Action journal entries atomically (architecture §9). ROP Core
// never reads provider business tables.
func (s *Store) DB() *sql.DB { return s.db }

// BeginTx starts a transaction for atomic multi-table writes.
func (s *Store) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return s.db.BeginTx(ctx, nil)
}

// Migrate applies pending migrations from migrationsDir, in filename order.
// Applied filenames are recorded in schema_migrations.
func (s *Store) Migrate(ctx context.Context, migrationsDir string) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		filename TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("store: schema_migrations: %w", err)
	}
	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("store: read migrations dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		var exists int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE filename = ?`, name).Scan(&exists); err != nil {
			return fmt.Errorf("store: check migration %s: %w", name, err)
		}
		if exists > 0 {
			continue
		}
		sqlBytes, err := os.ReadFile(filepath.Join(migrationsDir, name))
		if err != nil {
			return fmt.Errorf("store: read migration %s: %w", name, err)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("store: begin migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: apply migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (filename, applied_at) VALUES (?, ?)`,
			name, time.Now().UTC().Format(timeFormat)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit migration %s: %w", name, err)
		}
	}
	return nil
}
