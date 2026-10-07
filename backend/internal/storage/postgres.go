// Package storage is the PostgreSQL-backed implementation of the repository
// interfaces owned by the domain packages (accounting.Repository,
// sync.PullStore). It is the only package that imports pgx; domain code never
// does, so the accounting transaction boundary stays testable without a
// database.
package storage

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ledgerlink/branchledger/backend/internal/accounting"
)

// DB wraps a pgx connection pool and implements accounting.Repository.
type DB struct {
	pool *pgxpool.Pool
}

// Open connects to PostgreSQL using connString (e.g.
// "postgres://user:pass@host:5432/branchledger?sslmode=disable").
func Open(ctx context.Context, connString string) (*DB, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, fmt.Errorf("storage: open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("storage: ping: %w", err)
	}
	return &DB{pool: pool}, nil
}

func (db *DB) Close() { db.pool.Close() }

// WithTx implements accounting.Repository: it opens one PostgreSQL
// transaction, runs fn against a Tx bound to it, and commits only on nil
// error — matching the architecture's "begin, ..., commit; rollback removes
// the complete attempted posting if validation or persistence fails."
func (db *DB) WithTx(ctx context.Context, fn func(ctx context.Context, tx accounting.Tx) error) error {
	pgxTx, err := db.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("storage: begin tx: %w", err)
	}

	txImpl := &txImpl{tx: pgxTx}
	if err := fn(ctx, txImpl); err != nil {
		if rbErr := pgxTx.Rollback(ctx); rbErr != nil && rbErr != pgx.ErrTxClosed {
			return fmt.Errorf("storage: rollback after %w: %v", err, rbErr)
		}
		return err
	}

	if err := pgxTx.Commit(ctx); err != nil {
		return fmt.Errorf("storage: commit: %w", err)
	}
	return nil
}
