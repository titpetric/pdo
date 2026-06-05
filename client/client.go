package client

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// Client wraps an *sqlx.DB and provides a PHP-PDO-like API.
//
// Concurrency: The underlying *sqlx.DB is safe for concurrent use, so multiple
// PDO instances can share the same database connection pool. A single *Client
// value is NOT safe for concurrent use: write operations mutate lastInsertID
// and rowsAffected, and transaction methods mutate transaction state. For
// stateless HTTP handlers, create a new *Client per request that shares the
// underlying *sql.DB pool (see OpenDB).
type Client struct {
	db           *sqlx.DB
	conn         *sqlx.Conn
	tx           *sqlx.Tx
	lastInsertID int64
	rowsAffected int64
	observe      ObserveFunc
}

// Close closes the underlying exclusive connection.
// Only needed does something if `Connect()` is called.
func (p *Client) Close() error {
	if p.conn != nil {
		err := p.conn.Close()
		p.conn = nil
		return err
	}
	return nil
}

// NewClient creates a new PDO from an existing *sqlx.DB.
func NewClient(db *sqlx.DB) *Client {
	return &Client{
		db: db,
	}
}

// WithObserver sets an observer to the client.
func (p *Client) WithObserver(fn ObserveFunc) {
	p.observe = fn
}

// Insert inserts a struct into the table. The struct must have `db` tags.
// Uses NamedExecContext to bind struct fields to :name placeholders.
func (p *Client) Insert(ctx context.Context, table string, value any) error {
	cols, err := structColumns(value)
	if err != nil {
		return err
	}
	placeholders := make([]string, len(cols))
	for i, c := range cols {
		placeholders[i] = ":" + c
	}
	query := "INSERT INTO " + table + " (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
	return p.exec(ctx, query, value)
}

// Replace performs a REPLACE INTO operation with a struct.
func (p *Client) Replace(ctx context.Context, table string, value any) error {
	cols, err := structColumns(value)
	if err != nil {
		return err
	}
	placeholders := make([]string, len(cols))
	for i, c := range cols {
		placeholders[i] = ":" + c
	}
	query := "REPLACE INTO " + table + " (" + strings.Join(cols, ", ") + ") VALUES (" + strings.Join(placeholders, ", ") + ")"
	return p.exec(ctx, query, value)
}

// Update updates rows using a struct. keyCols specify WHERE columns.
func (p *Client) Update(ctx context.Context, table string, value any, keyCols ...string) error {
	cols, err := structColumns(value)
	if err != nil {
		return err
	}

	keySet := make(map[string]bool, len(keyCols))
	for _, k := range keyCols {
		keySet[k] = true
	}

	var setParts []string
	for _, c := range cols {
		if !keySet[c] {
			setParts = append(setParts, c+" = :"+c)
		}
	}

	var whereParts []string
	for _, k := range keyCols {
		whereParts = append(whereParts, k+" = :"+k)
	}

	query := "UPDATE " + table + " SET " + strings.Join(setParts, ", ") + " WHERE " + strings.Join(whereParts, " AND ")
	return p.exec(ctx, query, value)
}

// Exec executes a query with args (positional, map, or struct for named params).
func (p *Client) Exec(ctx context.Context, query string, args ...any) error {
	return p.exec(ctx, query, args...)
}

// Select scans all results into dest (pointer to slice of structs).
func (p *Client) Select(ctx context.Context, dest any, query string, args ...any) error {
	return p.selectRows(ctx, dest, query, args...)
}

// Get scans the first result into dest (pointer to struct).
func (p *Client) Get(ctx context.Context, dest any, query string, args ...any) error {
	return p.getRow(ctx, dest, query, args...)
}

// InsertID returns the last insert ID.
func (p *Client) InsertID() int64 {
	return p.lastInsertID
}

// RowsAffected returns the number of rows affected by the last write operation.
func (p *Client) RowsAffected() int64 {
	return p.rowsAffected
}

// Connect takes a transaction from the pool for exclusive use by the client.
func (p *Client) Connect(ctx context.Context) error {
	conn, err := p.db.Connx(ctx)
	if err != nil {
		return err
	}

	p.conn = conn
	return nil
}

// Begin starts a transaction. Returns an error if a transaction is already
// in progress (nested transactions are not supported).
func (p *Client) Begin(ctx context.Context) error {
	if p.tx != nil {
		return fmt.Errorf("pdodb: transaction already in progress")
	}

	type beginner interface {
		BeginTxx(context.Context, *sql.TxOptions) (*sqlx.Tx, error)
	}

	var db beginner

	db = p.db
	if p.conn != nil {
		db = p.conn
	}

	tx, err := db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("database error: can't start transaction: %w", err)
	}
	p.tx = tx
	return nil
}

// Rollback rolls back the current transaction. Returns nil when no transaction
// is in progress.
func (p *Client) Rollback() error {
	if p.tx == nil {
		return nil
	}
	err := p.tx.Rollback()
	p.tx = nil
	return err
}

// Commit commits the current transaction. Returns nil when no transaction is
// in progress.
func (p *Client) Commit() error {
	if p.tx == nil {
		return nil
	}
	err := p.tx.Commit()
	p.tx = nil
	return err
}

// --- Internal implementations ---

// structColumns extracts column names from a struct's `db` tags.
func structColumns(value any) ([]string, error) {
	t := reflect.TypeOf(value)
	if t == nil {
		return nil, fmt.Errorf("pdodb: nil value")
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("pdodb: value must be a struct, got %T", value)
	}

	var cols []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if tag := field.Tag.Get("db"); tag != "" && tag != "-" {
			cols = append(cols, tag)
		}
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("pdodb: no db-tagged fields found in %T", value)
	}
	return cols, nil
}

// isNamedArg reports whether arg should be bound as named parameters
// (struct or map[string]any/string), as opposed to positional parameters.
func isNamedArg(arg any) bool {
	if arg == nil {
		return false
	}
	if _, ok := arg.(map[string]string); ok {
		return true
	}
	if _, ok := arg.(map[string]any); ok {
		return true
	}
	t := reflect.TypeOf(arg)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t.Kind() == reflect.Struct
}

func (p *Client) txDepth() int {
	if p.tx != nil {
		return 1
	}
	return 0
}

func (p *Client) log(ctx context.Context, query string, args any, started time.Time, err error) {
	if p.observe == nil {
		return
	}
	p.observe(ctx, QueryLogEntry{
		Query:    query,
		Args:     args,
		Started:  started,
		Duration: time.Since(started),
		Err:      err,
		TxDepth:  p.txDepth(),
	})
}

// execer ignores 'conn' as it doesn't implement ExtContext
// and is missing named bindings. Could be awkward.
func (p *Client) execer() sqlx.ExtContext {
	if p.tx != nil {
		return p.tx
	}
	return p.db
}

func (p *Client) queryer() interface {
	SelectContext(ctx context.Context, dest any, query string, args ...any) error
	GetContext(ctx context.Context, dest any, query string, args ...any) error
} {
	if p.tx != nil {
		return p.tx
	}
	if p.conn != nil {
		return p.conn
	}
	return p.db
}

func (p *Client) exec(ctx context.Context, query string, args ...any) error {
	started := time.Now()
	var (
		res     sql.Result
		err     error
		logArgs any = args
	)
	if len(args) == 1 && isNamedArg(args[0]) {
		logArgs = args[0]
		res, err = sqlx.NamedExecContext(ctx, p.execer(), query, args[0])
	} else {
		res, err = p.execer().ExecContext(ctx, query, args...)
	}
	p.log(ctx, query, logArgs, started, err)
	if err != nil {
		return fmt.Errorf("%w\nquery: %s", err, query)
	}

	if id, idErr := res.LastInsertId(); idErr == nil {
		p.lastInsertID = id
	}
	if rows, rowsErr := res.RowsAffected(); rowsErr == nil {
		p.rowsAffected = rows
	}
	return nil
}

// bindNamed expands a named query against a struct or map and rebinds it to
// the driver's placeholder style. Used for Select/Get since sqlx's named query
// helpers don't compose cleanly with GetContext/SelectContext.
func (p *Client) bindNamed(query string, arg any) (string, []any, error) {
	q, bound, err := sqlx.Named(query, arg)
	if err != nil {
		return "", nil, err
	}
	return p.db.Rebind(q), bound, nil
}

func (p *Client) selectRows(ctx context.Context, dest any, query string, args ...any) error {
	q, boundArgs, logArgs, err := p.prepareQuery(query, args...)
	if err != nil {
		return fmt.Errorf("%w\nquery: %s", err, query)
	}
	started := time.Now()
	err = p.queryer().SelectContext(ctx, dest, q, boundArgs...)
	p.log(ctx, query, logArgs, started, err)
	if err != nil {
		return fmt.Errorf("%w\nquery: %s", err, query)
	}
	return nil
}

func (p *Client) getRow(ctx context.Context, dest any, query string, args ...any) error {
	q, boundArgs, logArgs, err := p.prepareQuery(query, args...)
	if err != nil {
		return fmt.Errorf("%w\nquery: %s", err, query)
	}
	started := time.Now()
	err = p.queryer().GetContext(ctx, dest, q, boundArgs...)
	p.log(ctx, query, logArgs, started, err)
	if err != nil {
		return fmt.Errorf("%w\nquery: %s", err, query)
	}
	return nil
}

func (p *Client) prepareQuery(query string, args ...any) (string, []any, any, error) {
	if len(args) == 1 && isNamedArg(args[0]) {
		q, bound, err := p.bindNamed(query, args[0])
		return q, bound, args[0], err
	}
	return query, args, args, nil
}
