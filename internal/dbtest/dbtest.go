// Package dbtest opens the database that a test should run against.
//
// Every test in this repo used to open SQLite directly, which meant the
// Postgres half of internal/db had never once been executed: `sqlc generate`
// emits both dialects and there is a hand-written adapter for each, so a query
// change was verified on Postgres only by compiling. That catches a type error
// and nothing else - not a wrong column order, not a dialect-specific NULL, not
// an adapter that reads the wrong field. todo.md had the measured example:
// SearchUsers lowercases its pattern purely so a LOWER() on the column matches
// on Postgres, and deleting that normalisation left every test green.
//
// So the driver is a decision made here, once, from the environment:
//
//	go test ./...                                        # SQLite, as before
//	CARAVEL_TEST_DB_DRIVER=postgres \
//	  CARAVEL_TEST_DB_DSN=postgres://... go test ./...    # the other dialect
//
// `make test-postgres` sets both and brings the container up first.
package dbtest

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"caravel/internal/db"
)

const (
	// DriverEnv selects the dialect: empty or "sqlite" keeps the old
	// behaviour, "postgres" needs DSNEnv as well.
	DriverEnv = "CARAVEL_TEST_DB_DRIVER"
	// DSNEnv is the *server* DSN for Postgres - a database that already
	// exists, into which each test creates its own schema. Ignored by SQLite,
	// which gets a file per test either way.
	DSNEnv = "CARAVEL_TEST_DB_DSN"
)

// schemaSeq numbers the per-test Postgres schemas within one test binary.
//
// The counter alone is not enough to name them: `go test ./...` runs each
// package as its own process, so two packages both start at 1. That collision
// is not theoretical - internal/auth and internal/httpapi raced for
// caravel_test_1 and one migrated a schema the other had just dropped. Hence
// schemaPrefix, which is per *process*.
var schemaSeq atomic.Uint64

// schemaPrefix is unique to this test binary: the pid, plus random bytes in
// case a pid is reused between runs (a killed run can leave schemas behind, and
// reusing one of those names would resurrect its tables).
var schemaPrefix = func() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Cannot fail in practice; the pid alone is still unique among
		// concurrently running processes, which is what matters here.
		return fmt.Sprintf("caravel_test_%d", os.Getpid())
	}
	return fmt.Sprintf("caravel_test_%d_%s", os.Getpid(), hex.EncodeToString(b[:]))
}()

// One admin pool for the whole test binary, not one per test.
//
// Postgres allows 100 connections by default and internal/httpapi alone opens
// several hundred databases in a run; a pool per test exhausted the server
// around the 160th ("sorry, too many clients already") long before any dialect
// difference could be observed. This pool only creates and drops schemas, so
// one connection is plenty.
var (
	adminOnce sync.Once
	adminPool *sql.DB
	adminErr  error
)

func admin(dsn string) (*sql.DB, error) {
	adminOnce.Do(func() {
		adminPool, adminErr = sql.Open("pgx", dsn)
		if adminErr != nil {
			return
		}
		adminPool.SetMaxOpenConns(2)
		adminPool.SetMaxIdleConns(1)
		if adminErr = adminPool.Ping(); adminErr != nil {
			adminPool.Close()
			adminPool = nil
		}
	})
	return adminPool, adminErr
}

// Open returns a migrated, empty database for one test, along with the driver
// name its store should be built with. Cleanup is registered on t.
//
// There is deliberately no fallback: asking for Postgres and silently getting
// SQLite is how a CI job reports a green run for a dialect it never touched,
// which is the exact failure this package exists to remove.
func Open(t *testing.T) (string, *sql.DB) {
	t.Helper()

	switch driver := strings.TrimSpace(os.Getenv(DriverEnv)); driver {
	case "", "sqlite":
		return "sqlite", openSQLite(t)
	case "postgres":
		return "postgres", openPostgres(t)
	default:
		t.Fatalf("%s=%q is not a driver: use %q or %q", DriverEnv, driver, "sqlite", "postgres")
		return "", nil
	}
}

// openSQLite is what every test did inline before this package existed: a fresh
// file under the test's own temp dir, thrown away with it.
func openSQLite(t *testing.T) *sql.DB {
	t.Helper()

	conn, err := db.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// openPostgres gives the test its own schema inside the shared database.
//
// A schema rather than a database because CREATE DATABASE cannot run inside a
// transaction and is slow enough to notice a few hundred times; the migrator
// follows along for free, since golang-migrate's Postgres driver takes its
// target schema from current_schema() and pgx applies search_path from the DSN.
func openPostgres(t *testing.T) *sql.DB {
	t.Helper()

	dsn := strings.TrimSpace(os.Getenv(DSNEnv))
	if dsn == "" {
		t.Fatalf("%s=postgres needs %s (e.g. postgres://caravel:caravel@localhost:5432/caravel?sslmode=disable)", DriverEnv, DSNEnv)
	}

	adminConn, err := admin(dsn)
	if err != nil {
		t.Fatalf("cannot reach postgres at %s: %v\nis the container up? try `make test-postgres`", redact(dsn), err)
	}

	schema := fmt.Sprintf("%s_%d", schemaPrefix, schemaSeq.Add(1))
	// No DROP-if-exists before this: the name is unique to this process, so
	// anything already using it would be someone else's schema. An earlier
	// version dropped first "in case a killed run left one behind", and
	// promptly deleted a concurrent package's database instead. Leftovers from
	// killed runs are swept by scripts/test_postgres.sh, where there is no
	// concurrency to get wrong.
	if _, err := adminConn.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatalf("create schema %s: %v", schema, err)
	}

	scoped, err := withSearchPath(dsn, schema)
	if err != nil {
		t.Fatalf("build dsn: %v", err)
	}
	conn, err := db.Open("postgres", scoped)
	if err != nil {
		t.Fatalf("open postgres schema %s: %v", schema, err)
	}
	// Kept small for the same reason the admin pool is shared: one test needs a
	// couple of connections at most (a handler, and the migrator that already
	// finished), and hundreds of tests share a server that allows 100.
	conn.SetMaxOpenConns(3)
	conn.SetMaxIdleConns(1)
	conn.SetConnMaxIdleTime(5 * time.Second)

	t.Cleanup(func() {
		conn.Close()
		if _, err := adminConn.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Errorf("drop schema %s: %v", schema, err)
		}
	})
	return conn
}

// withSearchPath adds search_path to a Postgres DSN. pgx passes unrecognised
// query parameters to the server as runtime parameters, which is what makes the
// per-test schema work without any change to db.Open.
func withSearchPath(dsn, schema string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		// Not a URL: Postgres also accepts keyword/value DSNs
		// ("host=... dbname=..."), where the same setting is a space-separated
		// key. Handled rather than rejected, since that form is what a
		// PGSERVICE-style configuration produces.
		if strings.Contains(dsn, "=") && !strings.Contains(dsn, "://") {
			return dsn + " search_path=" + schema, nil
		}
		return "", fmt.Errorf("parse %s: %w", DSNEnv, err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// redact keeps a password out of a failure message, which is otherwise the one
// place a CI log would print it.
func redact(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil || u.User == nil {
		return dsn
	}
	if _, hasPassword := u.User.Password(); hasPassword {
		u.User = url.UserPassword(u.User.Username(), "xxxxx")
	}
	return u.String()
}
