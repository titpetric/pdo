package tests

import (
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"

	"github.com/titpetric/pdo"
	"github.com/titpetric/pdo/schema"

	_ "modernc.org/sqlite"
)

func NewDB(tb testing.TB) *sqlx.DB {
	handle, err := sqlx.Open("sqlite", ":memory:")
	require.NoError(tb, err)
	tb.Cleanup(func() { handle.Close() })
	handle.SetMaxOpenConns(1)
	handle.MustExecContext(tb.Context(), schema.Migrations)
	return handle
}

func NewPDO(tb testing.TB) *pdo.PDO {
	tb.Helper()

	db := pdo.New(NewDB(tb))

	return db
}
