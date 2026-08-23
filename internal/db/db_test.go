package db_test

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"

	"caravel/internal/db"
	"caravel/internal/dbtest"
)

// Migrating must not cost a connection for the life of the pool.
//
// It used to. golang-migrate's Postgres driver checks a dedicated connection
// out of the pool it is handed (it needs one for the advisory lock) and returns
// it only when the migrator is closed, which db.Open never did - so every
// server held an extra idle connection forever, and every test in a run held
// one until Postgres refused the 100th client. That surfaced as "sorry, too
// many clients already" in whichever test happened to be running, which says
// nothing about the cause; this asserts the property directly instead.
//
// Postgres-only by nature: it is about connections to a server. On SQLite it
// skips, which is honest - there is nothing here to check.
func TestMigrationsDoNotHoldAConnection(t *testing.T) {
	if os.Getenv(dbtest.DriverEnv) != "postgres" {
		t.Skipf("needs %s=postgres: this is about server connections", dbtest.DriverEnv)
	}

	baseDSN := strings.TrimSpace(os.Getenv(dbtest.DSNEnv))
	if baseDSN == "" {
		t.Fatalf("%s is required with %s=postgres", dbtest.DSNEnv, dbtest.DriverEnv)
	}

	// This test manages its own schema rather than calling dbtest.Open,
	// because it needs a *label* on the connections it is counting.
	//
	// The first version counted every connection to the database, which passed
	// alone and failed in a full run: `go test ./...` runs each package as its
	// own process, so other packages' pools were part of the number. Counting
	// by application_name measures this pool and nothing else.
	const appName = "caravel_migration_leak_probe"
	schema := fmt.Sprintf("caravel_leak_probe_%d", os.Getpid())

	admin, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open admin: %v", err)
	}
	admin.SetMaxOpenConns(1)

	if _, err := admin.Exec(`DROP SCHEMA IF EXISTS ` + schema + ` CASCADE`); err != nil {
		t.Fatalf("drop stale schema: %v", err)
	}
	if _, err := admin.Exec(`CREATE SCHEMA ` + schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	// Closed here rather than by a defer: a deferred admin.Close() runs before
	// t.Cleanup does, which left the cleanup below talking to a closed pool.
	t.Cleanup(func() {
		if _, err := admin.Exec(`DROP SCHEMA ` + schema + ` CASCADE`); err != nil {
			t.Errorf("drop schema: %v", err)
		}
		admin.Close()
	})

	dsn, err := dsnWith(baseDSN, map[string]string{"search_path": schema, "application_name": appName})
	if err != nil {
		t.Fatalf("build dsn: %v", err)
	}

	// db.Open migrates. Everything it opens - including the migrator's own pool
	// - carries appName, so the count below sees exactly what it left behind.
	conn, err := db.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open and migrate: %v", err)
	}
	defer conn.Close()

	// With no idle connections allowed, every connection this pool owns is
	// closed the moment it is returned. So after one round trip, anything still
	// open is checked out and will never come back.
	conn.SetMaxIdleConns(0)
	var one int
	if err := conn.QueryRow(`SELECT 1`).Scan(&one); err != nil {
		t.Fatalf("select 1: %v", err)
	}

	var held int
	if err := admin.QueryRow(
		`SELECT count(*) FROM pg_stat_activity WHERE application_name = $1`, appName,
	).Scan(&held); err != nil {
		t.Fatalf("count connections: %v", err)
	}
	if held != 0 {
		t.Errorf(
			"migrating left %d connection(s) checked out of an idle pool\n"+
				"the migrator must run on a pool of its own and close it (see migratePostgres)",
			held,
		)
	}
}

// dsnWith adds runtime parameters to a Postgres URL. pgx passes unrecognised
// query parameters to the server, which is what makes both search_path and
// application_name work without any change to db.Open.
func dsnWith(dsn string, params map[string]string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
