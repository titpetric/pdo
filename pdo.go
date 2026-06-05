package pdo

import (
	"context"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/titpetric/pdo/client"
)

// PDO implements the client with a go generics 1.27+ method API.
type PDO struct {
	client driver
}

// New creates a new database client for exclusive use.
func New(db *sqlx.DB) *PDO {
	return &PDO{
		client: client.NewClient(db),
	}
}

// WithObserver passes along an observer to the client.
func (h *PDO) WithObserver(observerFn client.ObserveFunc) {
	h.client.WithObserver(observerFn)
}

// InsertID returns the ID of the last inserted row.
func (h *PDO) InsertID() int64 {
	return h.client.InsertID()
}

// RowsAffected returns the number of rows affected by the statement.
func (h *PDO) RowsAffected() int64 {
	return h.client.RowsAffected()
}

// Begin starts a transaction.
func (h *PDO) Begin(ctx context.Context) error {
	return h.client.Begin(ctx)
}

// Commit will write out transaction data.
func (h *PDO) Commit() error {
	return h.client.Commit()
}

// Rollback will rollback the transaction, reverting it in case of error.
func (h *PDO) Rollback() error {
	return h.client.Rollback()
}

// Transaction is a helper to run some code in transaction context.
// It will retry the transaction commit once with 50ms delay.
func (h *PDO) Transaction(ctx context.Context, txfn func(ctx context.Context) error) error {
	if err := h.Begin(ctx); err != nil {
		return err
	}

	if err := txfn(ctx); err != nil {
		h.Rollback()
		return err
	}

	if err := h.Commit(); err != nil {
		time.Sleep(50 * time.Millisecond)
		return h.Commit()
	}
	return nil
}

// Insert inserts a value into the table with compile-time type safety.
func (h *PDO) Insert[T any](ctx context.Context, table string, value T) error {
	return h.client.Insert(ctx, table, value)
}

// Replace performs a REPLACE INTO with compile-time type safety.
func (h *PDO) Replace[T any](ctx context.Context, table string, value T) error {
	return h.client.Replace(ctx, table, value)
}

// Update updates rows with compile-time type safety.
func (h *PDO) Update[T any](ctx context.Context, table string, value T, keyCols ...string) error {
	return h.client.Update(ctx, table, value, keyCols...)
}

// Exec runs a custom query to insert or modify data. It allows a bulk insert/update.
func (h *PDO) Exec(ctx context.Context, query string, args ...any) error {
	return h.client.Exec(ctx, query, args...)
}

// Select executes a query and returns all results as []T.
func (h *PDO) Select[T any](ctx context.Context, query string, args ...any) ([]T, error) {
	var results []T
	if err := h.client.Select(ctx, &results, query, args...); err != nil {
		return nil, err
	}
	return results, nil
}

// Get executes a query and returns the first result as T.
// Returns error if no rows found.
func (h *PDO) Get[T any](ctx context.Context, query string, args ...any) (*T, error) {
	var result T
	if err := h.client.Get(ctx, &result, query, args...); err != nil {
		return nil, err
	}
	return &result, nil
}
