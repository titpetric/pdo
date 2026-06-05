package pdo

import (
	"context"

	"github.com/titpetric/pdo/client"
)

// Connector manages an exclusive connection taken from the pool.
type Connector interface {
	// Connect takes an exclusive connection from the pool.
	Connect(ctx context.Context) error
	// Close returns the exclusive connection to the pool.
	Close() error
}

// Transactor contains mutators to begin, commit and rollback transaction.
type Transactor interface {
	// Begin starts a transaction.
	Begin(ctx context.Context) error
	// Rollback rolls back the transaction.
	Rollback() error
	// Commit commits the transaction.
	Commit() error
}

// Observer allows to define an observer to collect queries
// that executed during the request into an in-memory log.
type Observer interface {
	WithObserver(client.ObserveFunc)
}

// QueryResultState contains introspection for the last ran query.
type QueryResultState interface {
	// InsertID returns the last insert ID from an insert operation.
	InsertID() int64
	// RowsAffected returns the number of rows affected by the last write operation.
	RowsAffected() int64
}

// Writer contains write operations for storage.
// Delete is intentionally not implemented (soft deletes).
type Writer[T any] interface {
	// Insert inserts a struct into the table.
	Insert(ctx context.Context, table string, value T) error
	// Replace performs a REPLACE INTO operation.
	Replace(ctx context.Context, table string, value T) error
	// Update updates rows using a struct.
	Update(ctx context.Context, table string, value T, keyCols ...string) error
	// Exec executes a query with args.
	Exec(ctx context.Context, query string, args ...any) error
}

// Reader contains read operations for storage.
type Reader[T any] interface {
	// Select returns all results.
	Select(ctx context.Context, query string, args ...any) ([]T, error)
	// Get returns the first result.
	Get(ctx context.Context, query string, args ...any) (T, error)
}

// Handle is a complete client interface.
// It's not in use as we can't assert on generic methods.
type Handle[T any] interface {
	QueryResultState
	Transactor
	Observer

	Writer[T]
	Reader[T]
}
