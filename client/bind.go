package client

import (
	"database/sql"
	"reflect"
	"strconv"
	"strings"
)

// bindType is the placeholder style a driver reads.
type bindType int

const (
	bindQuestion bindType = iota // ?  (sqlite, mysql)
	bindDollar                   // $1 (postgres)
	bindAt                       // @p1 (sqlserver)
	bindNamed                    // :arg1 (oracle)
)

// bindTypeFor maps a driver name to its placeholder style. Unknown drivers
// read `?`.
func bindTypeFor(driverName string) bindType {
	switch driverName {
	case "postgres", "pgx", "pq-timeouts", "cloudsqlpostgres", "ql", "nrpostgres", "cockroach":
		return bindDollar
	case "mssql", "sqlserver":
		return bindAt
	case "oci8", "ora", "goracle", "godror":
		return bindNamed
	}
	return bindQuestion
}

// detectDriver names the driver behind db from the package path of its
// driver type, since *sql.DB does not expose the name it was opened with.
// Use WithDriver where this guesses wrong.
func detectDriver(db *sql.DB) string {
	if db == nil {
		return ""
	}
	t := reflect.TypeOf(db.Driver())
	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t == nil {
		return ""
	}
	path := strings.ToLower(t.PkgPath())
	switch {
	case strings.Contains(path, "pgx"), strings.Contains(path, "lib/pq"), strings.Contains(path, "cockroach"):
		return "pgx"
	case strings.Contains(path, "mssql"), strings.Contains(path, "sqlserver"):
		return "sqlserver"
	case strings.Contains(path, "godror"), strings.Contains(path, "goracle"), strings.Contains(path, "oci8"):
		return "godror"
	}
	return ""
}

// rebind rewrites `?` placeholders to the style of bt. It is a scanner and
// not a parser: a `?` inside a string literal is rewritten too.
func rebind(bt bindType, query string) string {
	if bt == bindQuestion {
		return query
	}

	var b strings.Builder
	b.Grow(len(query) + 16)
	n := 0
	for i := 0; i < len(query); i++ {
		c := query[i]
		if c != '?' {
			b.WriteByte(c)
			continue
		}
		n++
		switch bt {
		case bindDollar:
			b.WriteByte('$')
		case bindAt:
			b.WriteString("@p")
		case bindNamed:
			b.WriteString(":arg")
		}
		b.WriteString(strconv.Itoa(n))
	}
	return b.String()
}
