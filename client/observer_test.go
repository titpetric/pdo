package client_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/titpetric/pdo/client"
	"github.com/titpetric/pdo/model"
	"github.com/titpetric/pdo/tests"
)

func TestObserverRecordsQueries(t *testing.T) {
	ctx := t.Context()
	obs := &client.Observer{}

	db := tests.NewPDO(t)
	db.WithObserver(obs.Observe)

	require.NoError(t, db.Insert(ctx, model.UserTable, model.User{ID: "l1", Name: "Log", Email: "log@t"}))
	users, err := db.Select[model.User](ctx, "SELECT id, name, email FROM user")
	require.NoError(t, err)
	require.Len(t, users, 1)
	require.Equal(t, users[0].ID, "l1")

	entries := obs.Entries()
	require.Len(t, entries, 2)
	for _, e := range entries {
		t.Logf("Recorded entry: %v", e)
	}
}

func TestObserverTxDepth(t *testing.T) {
	obs := &client.Observer{}

	db := tests.NewPDO(t)
	db.WithObserver(obs.Observe)

	ctx := t.Context()

	require.NoError(t, db.Begin(ctx))
	require.NoError(t, db.Insert(ctx, model.UserTable, model.User{ID: "d1", Name: "D", Email: "d@t"}))
	require.NoError(t, db.Commit())

	var depths []int
	for _, e := range obs.Entries() {
		if strings.HasPrefix(e.Query, "INSERT") {
			depths = append(depths, e.TxDepth)
		}
	}
	require.Equal(t, []int{1}, depths)
}

func TestInsertNamedValues(t *testing.T) {
	ctx := t.Context()
	db := client.NewClient(tests.NewDB(t))

	require.NoError(t, db.Insert(ctx, model.UserTable, map[string]any{
		"id":    "m1",
		"name":  "Map",
		"email": "map@t",
	}))
	require.NoError(t, db.Insert(ctx, model.UserTable, mappedValues{
		"id":    "m2",
		"name":  "Mapped",
		"email": "mapped@t",
	}))

	var users []model.User
	require.NoError(t, db.Select(ctx, &users, "SELECT id, name, email FROM user ORDER BY id"))
	require.Equal(t, []model.User{
		{ID: "m1", Name: "Map", Email: "map@t"},
		{ID: "m2", Name: "Mapped", Email: "mapped@t"},
	}, users)
}

type mappedValues map[string]any

func (v mappedValues) Map() map[string]any {
	return v
}
