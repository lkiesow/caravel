package db

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Migration 0010 adds the area category.
//
// On Postgres that is two ALTER TABLE lines. On SQLite a CHECK constraint
// cannot be altered at all, so the migration rebuilds the whole items table --
// and a rebuild is where rows go missing: every sub-resource references
// items(id) ON DELETE CASCADE, so dropping the old table with foreign keys
// still enforced would take the locations, links, tags and itinerary entries
// with it. That is what this pins.
//
// SQLite only, for the same reason as TestMigration0006FoldsTypeIntoTags: the
// dialect files differ here, but what needs testing is the rebuild, which only
// SQLite does.
func TestMigration0010AddsAreaAndKeepsEverything(t *testing.T) {
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

	// The shape before this migration, filled with an item carrying one of
	// each thing that hangs off it.
	if err := newM().Migrate(9); err != nil {
		t.Fatalf("migrate to 9: %v", err)
	}
	exec := func(q string) {
		t.Helper()
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id, username, display_name, created_at, updated_at) VALUES ('u','u','U','2026-01-01','2026-01-01')`)
	exec(`INSERT INTO trips (id, owner_id, title, created_at, updated_at) VALUES ('t','u','T','2026-01-01','2026-01-01')`)
	exec(`INSERT INTO items (id, trip_id, category, title, sort_order, created_at, updated_at) VALUES ('i','t','site','Kirkjufell',3,'2026-01-01','2026-01-01')`)
	exec(`INSERT INTO item_locations (id, item_id, lat, lng, address) VALUES ('l','i',64.9,-23.3,'Grundarfjordur')`)
	exec(`INSERT INTO item_links (id, item_id, url, sort_order) VALUES ('k','i','https://example.com',0)`)
	exec(`INSERT INTO item_tags (item_id, tag) VALUES ('i','landmark')`)

	if err := newM().Up(); err != nil && err != migrate.ErrNoChange {
		t.Fatalf("up to head: %v", err)
	}

	count := func(q string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	// The rebuild copies every column, not just the ones it is about.
	if n := count(`SELECT count(*) FROM items WHERE id='i' AND title='Kirkjufell' AND sort_order=3 AND category='site'`); n != 1 {
		t.Errorf("the item did not survive the rebuild intact (n=%d)", n)
	}
	for _, q := range []string{
		`SELECT count(*) FROM item_locations WHERE item_id='i'`,
		`SELECT count(*) FROM item_links WHERE item_id='i'`,
		`SELECT count(*) FROM item_tags WHERE item_id='i'`,
	} {
		if n := count(q); n != 1 {
			t.Errorf("%s = %d, want 1 -- the drop cascaded", q, n)
		}
	}
	// And the index the table carried is back.
	if n := count(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_items_trip_id_category'`); n != 1 {
		t.Errorf("idx_items_trip_id_category lost in the rebuild (n=%d)", n)
	}

	// The point of the whole thing: area is now a legal category, and the
	// constraint still refuses anything else.
	if _, err := conn.Exec(`INSERT INTO items (id, trip_id, category, title, created_at, updated_at) VALUES ('a','t','area','Snaefellsnes','2026-01-01','2026-01-01')`); err != nil {
		t.Fatalf("area rejected after 0010: %v", err)
	}
	if _, err := conn.Exec(`INSERT INTO items (id, trip_id, category, title, created_at, updated_at) VALUES ('x','t','nonsense','X','2026-01-01','2026-01-01')`); err == nil {
		t.Error("the CHECK constraint is gone: a nonsense category was accepted")
	}

	// Down folds areas into sites rather than failing on them, and leaves
	// everything else where it is.
	if err := newM().Migrate(9); err != nil {
		t.Fatalf("down to 9: %v", err)
	}
	if n := count(`SELECT count(*) FROM items WHERE id='a' AND category='site'`); n != 1 {
		t.Errorf("the area was not folded back into a site (n=%d)", n)
	}
	if n := count(`SELECT count(*) FROM item_tags WHERE item_id='i'`); n != 1 {
		t.Errorf("the down rebuild cascaded the tags away (n=%d)", n)
	}
}
