package client_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/titpetric/pdo/client"
	"github.com/titpetric/pdo/tests"
)

func TestBridgeGet(t *testing.T) {
	ctx := t.Context()
	db := client.NewBridge(tests.NewDB(t))

	ok, err := db.Query(ctx, "INSERT INTO user (id, name, email) VALUES (?, ?, ?)", "b1", "Bridge", "bridge@t")
	require.NoError(t, err)
	require.Equal(t, true, ok)

	row, err := db.Get(ctx, "SELECT id, name FROM user WHERE id = ?", "b1")
	require.NoError(t, err)
	require.Equal(t, map[string]any{"id": "b1", "name": "Bridge"}, row)

	rows, err := db.GetAll(ctx, "SELECT name FROM user ORDER BY id")
	require.NoError(t, err)
	require.Equal(t, []map[string]any{{"name": "Bridge"}}, rows)

	_, err = db.Get(ctx, "SELECT id FROM user WHERE id = ?", "missing")
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestBridgeObserver(t *testing.T) {
	ctx := t.Context()
	db := client.NewBridge(tests.NewDB(t))
	var entries []client.QueryLogEntry
	db.WithObserver(func(_ context.Context, entry client.QueryLogEntry) {
		entries = append(entries, entry)
	})

	_, err := db.Query(ctx, "INSERT INTO user (id, name, email) VALUES (?, ?, ?)", "observed", "Observed", "observed@t")
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "INSERT INTO user (id, name, email) VALUES (?, ?, ?)", entries[0].Query)
	require.Positive(t, entries[0].Duration)
}

func TestBridgeWriteAndTransactionSignatures(t *testing.T) {
	ctx := t.Context()
	db := client.NewBridge(tests.NewDB(t))

	value, err := db.Begin(ctx)
	require.NoError(t, err)
	require.Equal(t, true, value)

	value, err = db.Insert(ctx, "user", mappedValues{
		"id": "tx", "name": "Transaction", "email": "transaction@t",
	})
	require.NoError(t, err)
	require.Equal(t, true, value)

	value, err = db.RowsAffected(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, value)

	value, err = db.Commit(ctx)
	require.NoError(t, err)
	require.Equal(t, true, value)

	value, err = db.Connect(ctx)
	require.NoError(t, err)
	require.Equal(t, true, value)
	value, err = db.Close(ctx)
	require.NoError(t, err)
	require.Equal(t, true, value)
}
