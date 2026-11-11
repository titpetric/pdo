package tests

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/titpetric/pdo"
	"github.com/titpetric/pdo/schema"

	_ "modernc.org/sqlite"
)

func NewDB(tb testing.TB) *sql.DB {
	handle, err := sql.Open("sqlite", ":memory:")
	require.NoError(tb, err)
	tb.Cleanup(func() { handle.Close() })
	handle.SetMaxOpenConns(1)
	_, err = handle.ExecContext(tb.Context(), schema.Migrations)
	require.NoError(tb, err)
	return handle
}

func NewPDO(tb testing.TB) *pdo.PDO {
	tb.Helper()

	db := pdo.New(NewDB(tb))

	return db
}
