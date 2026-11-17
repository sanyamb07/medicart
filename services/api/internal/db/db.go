// Package db owns the PostgreSQL connection pool and transaction plumbing.
//
// Data access uses pgx/v5 directly with hand-written queries in each domain
// package's repo.go. See docs/ARCHITECTURE.md for why this was chosen over
// sqlc: a large share of MediCart's queries are dynamically shaped (catalogue
// filters, sorts, admin list filters) which sqlc models awkwardly, and one
// consistent style is easier for a single maintainer than two.
//
// Every query is parameterised. No user input is ever concatenated into SQL;
// where a query shape varies, only the fixed fragments are chosen in Go and
// the values still travel as bind parameters.
package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool is the application's handle on Postgres.
type Pool struct{ *pgxpool.Pool }

// Config configures the pool.
type Config struct {
	URL      string
	MaxConns int32
	MinConns int32
}

// Connect opens the pool and verifies it with a ping, so a bad DATABASE_URL
// fails at boot rather than on the first customer request.
func Connect(ctx context.Context, cfg Config) (*Pool, error) {
	pc, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if cfg.MaxConns > 0 {
		pc.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		pc.MinConns = cfg.MinConns
	}
	// Managed Postgres providers drop idle connections; recycling them
	// ourselves avoids surfacing those as request errors.
	pc.MaxConnLifetime = time.Hour
	pc.MaxConnIdleTime = 10 * time.Minute
	pc.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, pc)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return &Pool{pool}, nil
}

// Querier is satisfied by both *pgxpool.Pool and pgx.Tx, so repository methods
// can run either standalone or inside a transaction without duplication.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// InTx runs fn inside a transaction, committing on success and rolling back on
// error or panic. Order creation, inventory movement and payment settlement
// all go through here — they must be atomic or the books do not balance.
func (p *Pool) InTx(ctx context.Context, fn func(Querier) error) error {
	return p.inTx(ctx, pgx.TxOptions{}, fn)
}

// InSerializableTx uses SERIALIZABLE isolation and retries serialisation
// failures. Used where concurrent callers contend for the same stock rows.
func (p *Pool) InSerializableTx(ctx context.Context, fn func(Querier) error) error {
	const maxAttempts = 3
	var err error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		err = p.inTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable}, fn)
		if err == nil || !IsSerializationFailure(err) {
			return err
		}
		// Brief backoff before retrying a conflict.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt*15) * time.Millisecond):
		}
	}
	return fmt.Errorf("transaction failed after %d attempts: %w", maxAttempts, err)
}

func (p *Pool) inTx(ctx context.Context, opts pgx.TxOptions, fn func(Querier) error) error {
	tx, err := p.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			// Rollback on a context that is already cancelled would be a no-op,
			// so use a fresh short-lived one to guarantee the release.
			rbCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = tx.Rollback(rbCtx)
		}
	}()

	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	committed = true
	return nil
}

// ---- error classification ----

// ErrNoRows is re-exported so domain packages need not import pgx.
var ErrNoRows = pgx.ErrNoRows

// IsNoRows reports whether err means "nothing matched".
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// IsUniqueViolation reports a duplicate-key error (SQLSTATE 23505).
// Relied on for idempotency: inserting the same idempotency key twice is
// detected by the constraint rather than by a racy pre-check.
func IsUniqueViolation(err error) bool { return sqlState(err) == "23505" }

// IsForeignKeyViolation reports SQLSTATE 23503.
func IsForeignKeyViolation(err error) bool { return sqlState(err) == "23503" }

// IsCheckViolation reports SQLSTATE 23514 — e.g. the non-negative stock check.
func IsCheckViolation(err error) bool { return sqlState(err) == "23514" }

// IsSerializationFailure reports SQLSTATE 40001/40P01 (serialisation failure
// and deadlock), both of which are safe to retry.
func IsSerializationFailure(err error) bool {
	s := sqlState(err)
	return s == "40001" || s == "40P01"
}

// ConstraintName returns the violated constraint, for mapping a specific
// constraint to a specific user-facing message.
func ConstraintName(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.ConstraintName
	}
	return ""
}

func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// IsMarketGuard reports whether err came from the migration-000014 database
// guards: MK001 = product not sellable in the business's market, MK002 =
// prescription line in a market without Rx sales enabled, MK003 = order
// currency differs from the market currency (000015).
func IsMarketGuard(err error) bool {
	s := sqlState(err)
	return s == "MK001" || s == "MK002" || s == "MK003"
}
