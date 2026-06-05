package client

import (
	"context"
	"time"
)

// QueryLogEntry records a single query execution for observability.
type QueryLogEntry struct {
	Query    string
	Args     any
	Started  time.Time
	Duration time.Duration
	Err      error
	TxDepth  int
}

// ObserveFunc is the callback signature for query observation.
type ObserveFunc func(ctx context.Context, entry QueryLogEntry)

// Observer collects query log entries.
type Observer struct {
	entries []QueryLogEntry
}

// Observe records a query log entry.
func (o *Observer) Observe(ctx context.Context, entry QueryLogEntry) {
	o.entries = append(o.entries, entry)
}

// Entries returns the collected entries.
func (o *Observer) Entries() []QueryLogEntry {
	return o.entries
}

// Clear removes all collected entries.
func (o *Observer) Clear() {
	o.entries = nil
}
