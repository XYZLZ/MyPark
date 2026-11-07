package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNoTx is returned by MustTx when the context carries no transaction.
var ErrNoTx = errors.New("database: no transaction in context")

// DBTX is the query surface shared by *pgxpool.Pool and pgx.Tx. It is
// identical to dbgen.DBTX, so either satisfies sqlc's New.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TxRunnerPort is the port feature packages depend on to run work in a
// transaction.
type TxRunnerPort interface {
	InTx(ctx context.Context, fn func(context.Context) error) error
}

type txKey struct{}

// TxRunner runs functions inside a database transaction carried in the
// context.
type TxRunner struct {
	pool *pgxpool.Pool
}

// NewTxRunner returns a runner that begins transactions on pool.
func NewTxRunner(pool *pgxpool.Pool) *TxRunner { return &TxRunner{pool: pool} }

// InTx runs fn in a transaction and commits when fn returns nil. It rolls
// back when fn returns an error (returned unchanged) or panics (the panic
// propagates). The ctx passed to fn carries the transaction: use Conn or
// MustTx to reach it. A nested InTx reuses the outer transaction, so an
// error returned by the inner fn only rolls back when the caller propagates
// it. Keep fn short: no hashing, network or other slow work while it holds
// a connection.
//
// Writes that must survive a failing request commit first and return the
// error after InTx has returned.
func (r *TxRunner) InTx(ctx context.Context, fn func(context.Context) error) (err error) {
	if _, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return fn(ctx)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("database: begin transaction: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		// Detached so a cancelled request still rolls back promptly.
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()
	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("database: commit transaction: %w", err)
	}
	committed = true
	return nil
}

// Conn returns the transaction carried by ctx, or pool when there is none.
func Conn(ctx context.Context, pool *pgxpool.Pool) DBTX {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}

// MustTx returns the transaction carried by ctx, or ErrNoTx. Use it where
// work (enqueue, audit) must never run outside a transaction.
func MustTx(ctx context.Context) (pgx.Tx, error) {
	if tx, ok := ctx.Value(txKey{}).(pgx.Tx); ok {
		return tx, nil
	}
	return nil, ErrNoTx
}
