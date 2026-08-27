package client

import (
	"database/sql"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
)

// TestPrepareQueryRebind covers the placeholder style each driver reads. The
// `?` a caller writes is only correct on sqlite and mysql; pgx wants $1, $2,
// and a statement that is not rebound fails there with a syntax error rather
// than a bind error, which is why this is a table over driver names.
func TestPrepareQueryRebind(t *testing.T) {
	tests := []struct {
		name   string
		driver string
		query  string
		args   []any
		expect string
	}{
		{
			name:   "sqlite passes through",
			driver: "sqlite",
			query:  "SELECT id FROM user WHERE id = ? AND name = ?",
			args:   []any{1, "Ada"},
			expect: "SELECT id FROM user WHERE id = ? AND name = ?",
		},
		{
			name:   "mysql passes through",
			driver: "mysql",
			query:  "SELECT id FROM user WHERE id = ? AND name = ?",
			args:   []any{1, "Ada"},
			expect: "SELECT id FROM user WHERE id = ? AND name = ?",
		},
		{
			name:   "pgx numbers the placeholders",
			driver: "pgx",
			query:  "SELECT id FROM user WHERE id = ? AND name = ?",
			args:   []any{1, "Ada"},
			expect: "SELECT id FROM user WHERE id = $1 AND name = $2",
		},
		{
			// Rebind scans rather than parses, so it cannot tell a
			// placeholder from a question mark inside a literal. A
			// statement with nothing to bind is left alone, which is what
			// an SQL console pasting arbitrary text needs.
			name:   "no arguments leaves the statement alone",
			driver: "pgx",
			query:  "SELECT 'why?' AS q",
			args:   nil,
			expect: "SELECT 'why?' AS q",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := &Client{db: sqlx.NewDb(nil, test.driver)}

			query, bound, logged, err := p.prepareQuery(test.query, test.args...)

			assert.NoError(t, err)
			assert.Equal(t, test.expect, query)
			assert.Equal(t, test.args, bound)
			assert.Equal(t, test.args, logged)
		})
	}
}

// TestPrepareQueryNamed asserts the named path still wins over the positional
// one: a single map argument is expanded by name, not bound by position.
func TestPrepareQueryNamed(t *testing.T) {
	p := &Client{db: sqlx.NewDb(nil, "pgx")}

	arg := map[string]any{"id": 1}
	query, bound, logged, err := p.prepareQuery("SELECT id FROM user WHERE id = :id", arg)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT id FROM user WHERE id = $1", query)
	assert.Equal(t, []any{1}, bound)
	assert.Equal(t, arg, logged)
}

// A lone value database/sql binds on its own takes the positional path. The
// named path would find no :name placeholders to fill and hand the driver a
// statement with its `?` still in it, which the server reports as a syntax
// error rather than as a bind error, so the caller is told the wrong thing
// about the wrong statement.
//
// The case that turns up is time.Time, because a DATETIME column scans into
// one and the same value goes back in: `INSERT INTO events (at) VALUES (?)`
// binds exactly one value, and that value is a struct.
func TestPrepareQueryBindsLoneValueByPosition(t *testing.T) {
	instant := time.Date(2026, time.August, 26, 14, 48, 0, 0, time.UTC)
	tests := []struct {
		name string
		arg  any
	}{
		{"time", instant},
		{"time pointer", &instant},
		{"driver.Valuer", sql.NullString{String: "Ada", Valid: true}},
		{"driver.Valuer pointer", &sql.NullString{String: "Ada", Valid: true}},
		{"null time", sql.NullTime{Time: instant, Valid: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := &Client{db: sqlx.NewDb(nil, "pgx")}

			query, bound, logged, err := p.prepareQuery("INSERT INTO events (at) VALUES (?)", test.arg)

			assert.NoError(t, err)
			assert.Equal(t, "INSERT INTO events (at) VALUES ($1)", query)
			assert.Equal(t, []any{test.arg}, bound)
			assert.Equal(t, []any{test.arg}, logged)
		})
	}
}

// A struct that is a bag of fields keeps the named path, which is what
// Insert, Replace and Update are built on. This is the other side of
// TestPrepareQueryBindsLoneValueByPosition: the exclusions are for values the
// driver binds, not for structs in general.
func TestPrepareQueryNamedStruct(t *testing.T) {
	p := &Client{db: sqlx.NewDb(nil, "pgx")}

	arg := struct {
		ID int `db:"id"`
	}{ID: 1}
	query, bound, logged, err := p.prepareQuery("SELECT id FROM user WHERE id = :id", arg)

	assert.NoError(t, err)
	assert.Equal(t, "SELECT id FROM user WHERE id = $1", query)
	assert.Equal(t, []any{1}, bound)
	assert.Equal(t, arg, logged)
}
