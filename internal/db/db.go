package db

import (
	"database/sql"
	"embed"
	"fmt"
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
	if err := migratePostgres(conn); err != nil {
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

func migratePostgres(conn *sql.DB) error {
	src, err := iofs.New(postgresMigrations, "migrations/postgres")
	if err != nil {
		return err
	}
	target, err := postgres.WithInstance(conn, &postgres.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", target)
	if err != nil {
		return err
	}
	return runMigrations(m)
}

func runMigrations(m *migrate.Migrate) error {
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}
