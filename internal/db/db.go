package db

import (
	"database/sql"
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

//go:embed migrations/sqlite/*.sql
var sqliteMigrations embed.FS

//go:embed migrations/postgres/*.sql
var postgresMigrations embed.FS

// Open opens a database connection for the given driver ("sqlite" or "postgres")
// and runs all pending migrations before returning.
func Open(driver, dsn string) (*sql.DB, error) {
	switch driver {
	case "sqlite":
		return openSQLite(dsn)
	case "postgres":
		return openPostgres(dsn)
	default:
		return nil, fmt.Errorf("unknown db driver %q", driver)
	}
}

func openSQLite(path string) (*sql.DB, error) {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	// foreign_keys must be enabled explicitly per-connection in SQLite, or
	// ON DELETE CASCADE (used throughout the schema) silently does nothing.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := migrateSQLite(conn); err != nil {
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}
	return conn, nil
}

func openPostgres(dsn string) (*sql.DB, error) {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	// Migrations run on their own connection pool, which is closed the moment
	// they finish - see migratePostgres for why that matters.
	if err := migratePostgres(dsn); err != nil {
		return nil, fmt.Errorf("migrate postgres: %w", err)
	}
	return conn, nil
}

func migrateSQLite(conn *sql.DB) error {
	src, err := iofs.New(sqliteMigrations, "migrations/sqlite")
	if err != nil {
		return err
	}
	// NoTxWrap: SQLite treats PRAGMA foreign_keys as a no-op while a
	// transaction is open, but golang-migrate wraps each migration file in
	// one by default. Migrations that must disable FK enforcement (e.g. to
	// recreate a referenced table without cascading deletes) need the
	// pragma to take effect statement-by-statement, so migrations run
	// without an enclosing transaction. This trades away all-or-nothing
	// atomicity per migration file; migrations are tested before shipping.
	target, err := sqlite.WithInstance(conn, &sqlite.Config{NoTxWrap: true})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("iofs", src, "sqlite", target)
	if err != nil {
		return err
	}
	return runMigrations(m)
}

// migratePostgres opens a pool of its own rather than borrowing the caller's,
// and closes it when the migrations are done.
//
// The reason is not tidiness. golang-migrate's Postgres driver checks a
// dedicated connection out of whatever pool it is given (it needs one, for the
// advisory lock that stops two instances migrating at once) and only returns it
// when the migrator is closed. Handed the application's pool, that connection
// is never given back: every server holds an extra idle Postgres connection for
// its whole life, and - the way this was found - every test in a run holds one
// too, until the server refuses new clients at 100. Its own pool means
// m.Close() can close both the connection and the pool, because neither is
// anyone else's.
//
// Takes a DSN rather than a *sql.DB for exactly that reason: there is no way to
// hand back a borrowed connection, so it must not borrow one.
func migratePostgres(dsn string) error {
	migrationConn, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open postgres for migrations: %w", err)
	}
	// One connection is all the migrator uses, and a second would only sit
	// idle for as long as this function runs.
	migrationConn.SetMaxOpenConns(1)

	src, err := iofs.New(postgresMigrations, "migrations/postgres")
	if err != nil {
		migrationConn.Close()
		return err
	}
	target, err := postgres.WithInstance(migrationConn, &postgres.Config{})
	if err != nil {
		migrationConn.Close()
		return err
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", target)
	if err != nil {
		migrationConn.Close()
		return err
	}
	// m.Close() closes the source and the database driver, and the driver
	// closes both its held connection and migrationConn. Reported rather than
	// ignored: a failure here is a leaked connection, which is the bug this
	// function was restructured to fix.
	defer func() {
		if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
			log.Printf("caravel: closing the postgres migrator: source=%v database=%v", srcErr, dbErr)
		}
	}()
	return runMigrations(m)
}

func runMigrations(m *migrate.Migrate) error {
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}
