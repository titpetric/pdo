package client

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

func TestCompileNamed(t *testing.T) {
	q, names := compileNamed("SELECT a::text, 'x:y' FROM t WHERE id = :id AND n = :user.name")
	assert.Equal(t, "SELECT a::text, 'x:y' FROM t WHERE id = ? AND n = ?", q)
	assert.Equal(t, []string{"id", "user.name"}, names)
}

func TestBindNamedArgsMissing(t *testing.T) {
	_, _, err := bindNamedArgs("SELECT :nope", map[string]any{"id": 1})
	assert.ErrorContains(t, err, "could not find name nope")
}

func TestRebindStyles(t *testing.T) {
	assert.Equal(t, "a = @p1 AND b = @p2", rebind(bindTypeFor("sqlserver"), "a = ? AND b = ?"))
	assert.Equal(t, "a = :arg1", rebind(bindTypeFor("godror"), "a = ?"))
	assert.Equal(t, "a = ?", rebind(bindTypeFor("sqlite"), "a = ?"))
}

type scanEmbedded struct {
	ID int `db:"id"`
}

type scanRowT struct {
	scanEmbedded
	Name    string `db:"name"`
	Created time.Time
	Skipped string `db:"-"`
}

func TestScan(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	c := NewClient(db)
	ctx := t.Context()

	require.NoError(t, c.Exec(ctx, "CREATE TABLE t (id INTEGER, name TEXT, created DATETIME)"))
	when := time.Date(2026, 11, 8, 10, 0, 0, 0, time.UTC)
	require.NoError(t, c.Insert(ctx, "t", map[string]any{"id": 1, "name": "a", "created": when}))
	require.NoError(t, c.Insert(ctx, "t", map[string]any{"id": 2, "name": "b", "created": when}))

	var rows []scanRowT
	require.NoError(t, c.Select(ctx, &rows, "SELECT id, name, created FROM t ORDER BY id"))
	require.Len(t, rows, 2)
	assert.Equal(t, 2, rows[1].ID)
	assert.True(t, rows[0].Created.Equal(when))

	var ptrs []*scanRowT
	require.NoError(t, c.Select(ctx, &ptrs, "SELECT id, name, created FROM t ORDER BY id"))
	assert.Equal(t, "b", ptrs[1].Name)

	var names []string
	require.NoError(t, c.Select(ctx, &names, "SELECT name FROM t ORDER BY id"))
	assert.Equal(t, []string{"a", "b"}, names)

	var one scanRowT
	require.NoError(t, c.Get(ctx, &one, "SELECT id, name, created FROM t WHERE id = :id", map[string]any{"id": 2}))
	assert.Equal(t, "b", one.Name)

	var ts time.Time
	require.NoError(t, c.Get(ctx, &ts, "SELECT created FROM t WHERE id = ?", 1))
	assert.True(t, ts.Equal(when))

	err = c.Get(ctx, &one, "SELECT id, name, created FROM t WHERE id = ?", 99)
	assert.ErrorIs(t, err, sql.ErrNoRows)

	err = c.Get(ctx, &one, "SELECT id, name, created, 1 AS extra FROM t WHERE id = ?", 1)
	assert.ErrorContains(t, err, "missing destination name extra")
}
