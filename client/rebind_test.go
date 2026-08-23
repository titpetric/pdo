package client

import (
	"testing"

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
