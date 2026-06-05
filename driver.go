package pdo

import (
	"context"
)

// Type driver is the typeless DB interface.
type driver interface {
	QueryResultState
	Connector
	Transactor
	Observer

	driverReader
	driverWriter
}

// driverWriter contains write operations for storage.
type driverWriter interface {
	// Insert inserts a struct into the table.
	Insert(ctx context.Context, table string, value any) error
	// Replace performs a REPLACE INTO operation.
	Replace(ctx context.Context, table string, value any) error
	// Update updates rows using a struct.
	Update(ctx context.Context, table string, value any, keyCols ...string) error
	// Exec executes a query with args.
	Exec(ctx context.Context, query string, args ...any) error
}

// driverReader contains read operations for storage.
type driverReader interface {
	// Select scans all results into dest.
	Select(ctx context.Context, dest any, query string, args ...any) error
	// Get scans the first result into dest.
	Get(ctx context.Context, dest any, query string, args ...any) error
}
