# pdo

`pdo` is a small, request-scoped SQL client for Go services. It fits the shape of
a normal `net/http` server: the standard library already gives every request its
own goroutine, so the natural unit of database state is one client per request.
Rather than threading connections, transactions, and "last insert id" bookkeeping
through every function call, you create one client at the top of a handler and use
it for the rest of that request.

The client handles query observation, pooling, and single-connection use. The API
is short and readable: it covers CRUD in an ORM-like way and falls back to plain
SQL for everything else.

A function that writes a row looks the same whether it runs on its own or as one
step inside a larger transaction. Typed reads use Go 1.27 generic methods
(`Get[T]`, `Select[T]`), so results scan straight into your structs and your
storage and repository packages stay simple.

> **Requires Go 1.27+ (`gotip`).** The public API uses *generic methods*, a
> language feature not yet in released Go. See [Building & testing](#building--testing).

## Requirements: sqlx

`pdo` is currently a thin layer over [`jmoiron/sqlx`](https://github.com/jmoiron/sqlx),
which it requires for setup. You own the `*sqlx.DB` connection pool - opening it,
configuring it, and running migrations - and you hand that pool to `pdo.New`.
The pool is safe for concurrent use; the per-request `*PDO` you create from it is
not. That's the whole point: it's single-goroutine, request-scoped state.

```go
import (
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// ExampleService holds the shared, concurrency-safe pool. It is created once
// at startup and lives for the life of the process.
type ExampleService struct {
	db *sqlx.DB
}

func NewExampleService() (*ExampleService, error) {
	db, err := sqlx.Open("sqlite", "file:app.db")
	if err != nil {
		return nil, err
	}
	return &ExampleService{db: db}, nil
}
```

## Request-scoped usage

Each handler creates its own client from the shared pool. Because `net/http`
runs every request on its own goroutine, the client never needs locking: it is
created, used, and discarded inside a single goroutine.

```go
type User struct {
	ID    string `db:"id"`
	Name  string `db:"name"`
	Email string `db:"email"`
}

func (s *ExampleService) GetUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db := pdo.New(s.db) // request-scoped client over the shared pool

	u, err := db.Get[User](ctx, "SELECT id, name, email FROM user WHERE id = ?", r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(u)
}
```

Reads and writes use the same client:

```go
func (s *ExampleService) CreateUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db := pdo.New(s.db)

	var u User
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Insert builds a parameterized INSERT from the struct's db tags.
	if err := db.Insert(ctx, "user", u); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
}
```

## Pinning a connection with Connect()

By default each query borrows a connection from the pool and returns it. When a
request needs several queries on the *same* physical connection - for session
settings, temporary tables, or read-your-write consistency on reads - pin one with
`Connect()` and release it with `Close()`:

```go
func (s *ExampleService) Report(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	db := pdo.New(s.db)

	if err := db.Connect(ctx); err != nil { // take an exclusive connection
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer db.Close() // return it to the pool

	// All reads below run on the pinned connection.
	rows, err := db.Select[User](ctx, "SELECT id, name, email FROM user ORDER BY id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(rows)
}
```

After `Connect()`, `Begin` starts its transaction on the pinned connection. Once
the transaction commits, the connection stays pinned until you call `Close()`.

> Note: reads run on the pinned connection, but writes still go through the pool,
> because `*sqlx.Conn` cannot bind named (`:name`) parameters. Don't pin the only
> connection of a single-connection pool and then write, or the write will block
> waiting for a connection that is already held.
>
> The simplest way to make named parameters work with Exec is to start a
> transaction before issuing those writes.

### Storage code is transaction-agnostic

The key property is that storage functions don't need to know whether a
transaction is active. They take the client and call `Insert`/`Update`/`Exec`
normally; the client routes the work to the open transaction if there is one, or
straight to the pool if there isn't. So the same function works both standalone
and as one step of a larger transaction, with no special "tx" parameter and no
duplicate transactional and non-transactional variants.

Start a transaction with `Begin`, finalize it with `Commit`, or revert it with
`Rollback`. Once a transaction is open on a client, every later query on that
client runs inside it until you commit or roll back.


```go
package storage

// createUser writes a single row. It is not transaction-aware.
func createUser(ctx context.Context, db *pdo.PDO, u User) error {
	return db.Insert(ctx, "user", u)
}

// addMembership writes a single row. Also not transaction-aware.
func addMembership(ctx context.Context, db *pdo.PDO, m Membership) error {
	return db.Insert(ctx, "membership", m)
}
```

Standalone, each call is its own implicit unit of work:

```go
// Runs on the pool, committed immediately by the driver.
if err := createUser(ctx, db, u); err != nil {
	return err
}
```

Composed, the *caller* opens the transaction and the very same functions now
participate in it automatically:

```go
func Signup(ctx context.Context, db *pdo.PDO, u User, m Membership) error {
	if err := db.Begin(ctx); err != nil {
		return err
	}
	defer db.Rollback()

	if err := createUser(ctx, db, u); err != nil { // joins the transaction
		return err
	}
	if err := addMembership(ctx, db, m); err != nil { // joins the transaction
		return err
	}

	return db.Commit()
}
```

This removes a lot of complexity from the storage layer. Most storage operations
are a single statement, and a single statement is just the smallest possible
transaction. By treating every interaction as transactional by default, the
storage package no longer needs two code paths (one taking a `*sql.Tx` and one
taking a `*sql.DB`); it exposes one consistent set of functions that compose
freely. The caller decides the boundary, and the functions don't change.

In practice a transaction is mostly a batch of `INSERT`/`UPDATE` statements. You
can interleave `SELECT`s inside one, but it's rare and usually worth avoiding so
read logic stays decoupled from write boundaries. Keep transactions short and
write-focused, and run reads outside them where you can.

## API reference

The entry point is `pdo.PDO`, created with `pdo.New(*sqlx.DB)`.

### Reads (generic)

| Method | Description |
| --- | --- |
| `Get[T](ctx, query, args...) (*T, error)` | Scan the first row into `*T`. Errors when no rows. |
| `Select[T](ctx, query, args...) ([]T, error)` | Scan all rows into `[]T`. |

### Writes

| Method | Description |
| --- | --- |
| `Insert[T](ctx, table, value)` | `INSERT INTO` built from `db` tags. |
| `Replace[T](ctx, table, value)` | `REPLACE INTO` built from `db` tags. |
| `Update[T](ctx, table, value, keyCols...)` | `UPDATE ... SET ... WHERE keyCols`. |
| `Exec(ctx, query, args...)` | Arbitrary write; supports bulk insert/update. |

After a write, `InsertID() int64` and `RowsAffected() int64` return state from
the last statement.

### Connection & transaction

| Method | Description |
| --- | --- |
| `Connect(ctx)` | Pin an exclusive connection from the pool. |
| `Close()` | Return the pinned connection; no-op if not pinned. |
| `Begin(ctx)` | Start a transaction (nested begins error). |
| `Commit()` | Commit changes made in transaction. |
| `Rollback()` | Roll back changes due to an error. |

### Parameter binding

Queries are always parameterized - never build SQL by string interpolation.
Two styles are supported and auto-detected:

- **Positional**: `"... WHERE id = ?"` with trailing args.
- **Named**: `"... WHERE id = :id"` with a single struct or `map[string]any` /
  `map[string]string` argument. Struct named params are driven by `db` tags.

### Observability

Attach an observer to record every executed query (query text, args, duration,
error, transaction depth):

```go
obs := &client.Observer{}

db.WithObserver(obs.Observe)
// ... run queries ...
for _, e := range obs.Entries() {
	log.Printf("%s (%s) err=%v", e.Query, e.Duration, e.Err)
}
```

## Project layout

| Path | Description |
| --- | --- |
| `pdo.go` | Public PDO type; generic method API (New, Get, Insert) |
| `interfaces.go` | Public interfaces (Reader, Writer, Transactor, ...) |
| `driver.go` | Internal typeless driver interface |
| `client/` | Client: sqlx wrapper, query building, observer |
| `model/` | Generated data models + query builders (mig) |
| `schema/` | SQL migrations + schema.yml |
| `tests/` | Shared test helpers + HTTP handler tests/benchmarks |
| `docs/` | Design notes on generic methods and DB access |

The `model/` package is generated by
[`go-bridget/mig`](https://github.com/go-bridget/mig) from `schema/schema.yml`
(`// Code generated ... DO NOT EDIT`). It exposes typed structs plus
`Insert()/Select()/Update()/Delete()` query builders.

## Building & testing

The public API depends on Go 1.27 generic methods, so a stable Go toolchain will
not compile it. Use `gotip`:

```sh
go install golang.org/dl/gotip@latest
gotip download

gotip build ./...
gotip test ./...
gotip test -bench=StatelessHTTPHandler -benchmem ./tests/
```

CI runs via [`atkins`](atkins.yml). The `default` task formats, tests (with
benchmarks and coverage), and prints a per-package coverage summary:

```sh
atkins   # runs gotip fmt + gotip test -bench=. -cover + coverage summary
```

## License

See [LICENSE](LICENSE).
