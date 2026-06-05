package pdo_test

import (
	"context"
	_ "embed"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/titpetric/pdo"
	"github.com/titpetric/pdo/model"
	"github.com/titpetric/pdo/tests"
)

var _ pdo.Transactor = (*pdo.PDO)(nil)
var _ pdo.QueryResultState = (*pdo.PDO)(nil)

// TestNamedQueries exercises the main use case: inserting, updating, replacing
// and selecting structs through named (:tag) bindings driven by db tags.
func TestNamedQueries(t *testing.T) {
	db := tests.NewPDO(t)
	ctx := context.Background()

	user := model.User{ID: "u1", Name: "Alice", Email: "alice@example.com"}

	// Insert via Insert() (builds named query from struct).
	require.NoError(t, db.Insert(ctx, model.UserTable, user))
	assert.EqualValues(t, 1, db.RowsAffected())

	// Insert via Exec() with a model-built query and struct named args.
	bob := model.User{ID: "u2", Name: "Bob", Email: "bob@example.com"}
	require.NoError(t, db.Exec(ctx, bob.Insert(), bob))

	// Get with struct-bound named params.
	got, err := db.Get[model.User](ctx, user.Select(model.WithWhere("id = :id")), struct {
		ID string `db:"id"`
	}{ID: "u1"})
	require.NoError(t, err)
	assert.Equal(t, user, *got)

	// Get with map-bound named params.
	got, err = db.Get[model.User](ctx, user.Select(model.WithWhere("email = :email")), map[string]any{"email": "bob@example.com"})
	require.NoError(t, err)
	assert.Equal(t, bob, *got)

	// Update via named query (db tags drive the SET / WHERE clauses).
	user.Email = "alice@new.com"
	require.NoError(t, db.Update(ctx, model.UserTable, user, "id"))

	// Replace via named query.
	bob.Email = "bob@new.com"
	require.NoError(t, db.Replace(ctx, model.UserTable, bob))

	// Select all and verify final state.
	all, err := db.Select[model.User](ctx, user.Select(model.WithOrderBy("id")))
	require.NoError(t, err)
	assert.Equal(t, []model.User{
		{ID: "u1", Name: "Alice", Email: "alice@new.com"},
		{ID: "u2", Name: "Bob", Email: "bob@new.com"},
	}, all)
}

func TestGetReturnsErrNoRows(t *testing.T) {
	db := tests.NewPDO(t)
	_, err := db.Get[model.User](t.Context(), "SELECT id, name, email FROM user WHERE id = ?", "nope")
	assert.Error(t, err)
}

func TestSelect(t *testing.T) {
	db := tests.NewPDO(t)
	ctx := context.Background()
	for _, name := range []string{"X", "Y", "Z"} {
		require.NoError(t, db.Insert(ctx, model.UserTable, model.User{ID: name, Name: name, Email: name + "@t"}))
	}
	names, err := db.Select[string](ctx, "SELECT name FROM user ORDER BY name")
	require.NoError(t, err)
	assert.Equal(t, []string{"X", "Y", "Z"}, names)
}

func TestTransactionCommit(t *testing.T) {
	db := tests.NewPDO(t)
	ctx := context.Background()

	require.NoError(t, db.Begin(ctx))
	require.NoError(t, db.Insert(ctx, model.UserTable, model.User{ID: "tx1", Name: "Tx", Email: "tx@t"}))
	require.NoError(t, db.Commit())

	got, err := db.Get[model.User](ctx, "SELECT id, name, email FROM user WHERE id = ?", "tx1")
	require.NoError(t, err)
	assert.Equal(t, "Tx", got.Name)
}

func TestTransactionRollback(t *testing.T) {
	db := tests.NewPDO(t)
	ctx := context.Background()

	require.NoError(t, db.Begin(ctx))
	require.NoError(t, db.Insert(ctx, model.UserTable, model.User{ID: "rb1", Name: "Rb", Email: "rb@t"}))
	require.NoError(t, db.Rollback())

	users, err := db.Select[model.User](ctx, "SELECT id, name, email FROM user WHERE id = ?", "rb1")
	require.NoError(t, err)
	assert.Empty(t, users)
}

func TestNestedBeginErrors(t *testing.T) {
	db := tests.NewPDO(t)
	ctx := context.Background()

	require.NoError(t, db.Begin(ctx))
	t.Cleanup(func() { _ = db.Rollback() })

	err := db.Begin(ctx)
	assert.Error(t, err, "nested Begin should error")
}

func TestCommitRollbackWithoutBegin(t *testing.T) {
	db := tests.NewPDO(t)
	assert.NoError(t, db.Commit())
	assert.NoError(t, db.Rollback())
}

func TestInsertNonStructFails(t *testing.T) {
	db := tests.NewPDO(t)
	err := db.Insert(context.Background(), model.UserTable, "not a struct")
	assert.Error(t, err)
}

func TestContextCancellation(t *testing.T) {
	db := tests.NewPDO(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := db.Insert(ctx, model.UserTable, model.User{ID: "c1", Name: "C", Email: "c@t"})
	assert.Error(t, err)
}

// TestStoragePattern demonstrates passing the Client interface to storage
// functions that work transparently in/out of transactions.
func TestStoragePattern(t *testing.T) {
	db := tests.NewPDO(t)
	ctx := context.Background()

	createUser := func(ctx context.Context, db *pdo.PDO, u model.User) error {
		return db.Insert(ctx, model.UserTable, u)
	}

	require.NoError(t, createUser(ctx, db, model.User{ID: "s1", Name: "U1", Email: "u1@t"}))

	require.NoError(t, db.Begin(ctx))
	require.NoError(t, createUser(ctx, db, model.User{ID: "s2", Name: "U2", Email: "u2@t"}))
	require.NoError(t, db.Commit())

	users, err := db.Select[model.User](ctx, "SELECT id, name, email FROM user ORDER BY id")
	require.NoError(t, err)
	assert.Len(t, users, 2)
}
