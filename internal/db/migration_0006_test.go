package db

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Migration 0006 folds items.type into item_tags and drops the column.
//
// This is the one migration in the tree that moves *data* rather than only
// reshaping the schema, and its interesting part is a guard that no amount of
// reading the SQL will confirm: the fold is skipped when the location already
// carries the same word as a tag differing only in case, because the primary
// key on (item_id, tag) is exact and would otherwise let "Hotel" and "hotel"
// sit on one location -- a state the application itself never produces.
//
// Migrates to 5 first so the old shape exists to migrate *from*. Kept rather
// than run once by hand: a fold that silently stops folding is invisible.
//
// SQLite only, deliberately. The two dialect files are byte-identical and
// check_migrations.py enforces that they stay so, and this opens its own SQLite
// database rather than the suite's, so it does the same work under
// `make test-postgres`. What it is testing is the SQL, not the driver.
func TestMigration0006FoldsTypeIntoTags(t *testing.T) {
	conn, err := Open("sqlite", t.TempDir()+"/m.db")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	newM := func() *migrate.Migrate {
		src, err := iofs.New(sqliteMigrations, "migrations/sqlite")
		if err != nil {
			t.Fatal(err)
		}
		target, err := sqlite.WithInstance(conn, &sqlite.Config{NoTxWrap: true})
		if err != nil {
			t.Fatal(err)
		}
		m, err := migrate.NewWithInstance("iofs", src, "sqlite", target)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}

	// Up to 0005 only: the schema as it was before this milestone.
	if err := newM().Migrate(5); err != nil {
		t.Fatalf("migrate to 5: %v", err)
	}

	exec := func(q string) {
		t.Helper()
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id, username, display_name, created_at, updated_at) VALUES ('u','u','U','2026-01-01','2026-01-01')`)
	exec(`INSERT INTO trips (id, owner_id, title, created_at, updated_at) VALUES ('t','u','T','2026-01-01','2026-01-01')`)
	item := func(id, typ string) {
		exec(`INSERT INTO items (id, trip_id, category, type, title, created_at, updated_at) VALUES ('` + id + `','t','site','` + typ + `','` + id + `','2026-01-01','2026-01-01')`)
	}
	item("plain", "museum")       // becomes a tag
	item("empty", "")             // contributes nothing
	item("padded", "  hotel  ")   // trimmed
	item("dupe", "Hotel")         // already tagged "hotel": skipped, case-insensitively
	item("alongside", "landmark") // keeps the tag it already had, and gains this one
	exec(`INSERT INTO item_tags (item_id, tag) VALUES ('dupe','hotel')`)
	exec(`INSERT INTO item_tags (item_id, tag) VALUES ('alongside','west')`)

	if err := newM().Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("up to head: %v", err)
	}

	tags := func(id string) []string {
		rows, err := conn.Query(`SELECT tag FROM item_tags WHERE item_id = ? ORDER BY tag`, id)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		return out
	}
	check := func(id string, want ...string) {
		t.Helper()
		got := tags(id)
		if len(got) != len(want) {
			t.Fatalf("%s: tags = %v, want %v", id, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: tags = %v, want %v", id, got, want)
			}
		}
	}
	check("plain", "museum")
	check("empty")
	check("padded", "hotel")
	check("dupe", "hotel") // NOT "Hotel" as well
	check("alongside", "landmark", "west")

	// The column is gone, and the index the table carries survived.
	if _, err := conn.Exec(`SELECT type FROM items`); err == nil {
		t.Error("items.type still exists after 0006")
	}
	var n int
	if err := conn.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_items_trip_id_category'`).Scan(&n); err != nil || n != 1 {
		t.Errorf("idx_items_trip_id_category lost: n=%d err=%v", n, err)
	}

	// Down puts the column back, empty, and leaves the tags alone.
	if err := newM().Steps(-1); err != nil {
		t.Fatalf("down: %v", err)
	}
	var empty int
	if err := conn.QueryRow(`SELECT count(*) FROM items WHERE type <> ''`).Scan(&empty); err != nil {
		t.Fatalf("type column not back: %v", err)
	}
	if empty != 0 {
		t.Errorf("down migration invented %d types; it cannot know them", empty)
	}
	check("plain", "museum")
	t.Log("0006 round-trips: types folded in, column dropped, down restores it empty")
}
