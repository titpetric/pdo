package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/require"

	"github.com/titpetric/pdo"
	"github.com/titpetric/pdo/client"
	"github.com/titpetric/pdo/model"
)

// allocHandler returns a handler that uses the generic Get[T] helper, which
// allocates a fresh T on every call and returns it by value.
func allocHandler(db *sqlx.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		pdb := pdo.New(db)

		id := r.URL.Query().Get("id")
		u, err := pdb.Get[model.User](ctx, "SELECT id, name, email FROM user WHERE id=?", id)

		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(u)
	}
}

// scannerHandler returns a handler that uses the scanner-style Client.Get,
// where the caller owns the destination value and passes a pointer to it.
func scannerHandler(db *sqlx.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		db := client.NewClient(db)
		var u model.User

		id := r.URL.Query().Get("id")
		err := db.Get(r.Context(), &u, "SELECT id, name, email FROM user WHERE id=?", id)

		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(u)
	}
}

const benchSeeded = 50

// setupBenchDB creates a shared :memory: database seeded with users. The pool
// is serialized to a single connection so the in-memory DB is shared across
// goroutines.
func setupBenchDB(tb testing.TB) *sqlx.DB {
	tb.Helper()
	ctx := tb.Context()

	db := NewDB(tb)

	setup := pdo.New(db)
	for i := 0; i < benchSeeded; i++ {
		id := fmt.Sprintf("u%d", i)
		require.NoError(tb, setup.Insert(ctx, model.UserTable, model.User{
			ID:    id,
			Name:  fmt.Sprintf("User%d", i),
			Email: id + "@test.com",
		}))
	}

	return db
}

// hammer runs concurrent GETs against url and validates the JSON body decodes
// into a User with the requested id. Used by both Test and Benchmark callers.
func hammer(tb testing.TB, client *http.Client, url string, n int) {
	tb.Helper()
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("u%d", i%benchSeeded)
		resp, err := client.Get(url + "?id=" + id)
		if err != nil {
			tb.Fatalf("get: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			tb.Fatalf("status %d for id=%s: %s", resp.StatusCode, id, body)
		}
		var got model.User
		if err := json.Unmarshal(body, &got); err != nil {
			tb.Fatalf("decode id=%s: %v", id, err)
		}
		if got.ID != id {
			tb.Fatalf("got id=%s want %s", got.ID, id)
		}
	}
}

// TestStatelessHTTPHandler smoke-tests both handler variants concurrently.
func TestStatelessHTTPHandler(t *testing.T) {
	db := setupBenchDB(t)

	for _, tc := range []struct {
		name    string
		handler func(*sqlx.DB) http.HandlerFunc
	}{
		{"alloc", allocHandler},
		{"scanner", scannerHandler},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(tc.handler(db))
			t.Cleanup(server.Close)
			hammer(t, server.Client(), server.URL, 200)
		})
	}
}

// BenchmarkStatelessHTTPHandler compares the allocating Get[T] generic helper
// against the scanner-style Client.Get under concurrent HTTP load.
//
// Run with: go test -bench=StatelessHTTPHandler -benchmem
func BenchmarkStatelessHTTPHandler(b *testing.B) {
	for _, bc := range []struct {
		name    string
		handler func(*sqlx.DB) http.HandlerFunc
	}{
		{"alloc", allocHandler},
		{"scanner", scannerHandler},
	} {
		b.Run(bc.name, func(b *testing.B) {
			db := setupBenchDB(b)
			server := httptest.NewServer(bc.handler(db))
			b.Cleanup(server.Close)

			client := server.Client()
			url := server.URL

			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					id := fmt.Sprintf("u%d", i%benchSeeded)
					i++
					resp, err := client.Get(url + "?id=" + id)
					if err != nil {
						b.Fatalf("get: %v", err)
					}
					_, _ = io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					if resp.StatusCode != http.StatusOK {
						b.Fatalf("status %d", resp.StatusCode)
					}
				}
			})
		})
	}
}
