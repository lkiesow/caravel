package db

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Migration 0011 adds the food, event and shop categories.
//
// Same shape as 0010, and the same risk: SQLite cannot alter a CHECK, so the
// items table is rebuilt, and a rebuild is where rows go missing -- every
// sub-resource references items(id) ON DELETE CASCADE, so dropping the old
// table with foreign keys still enforced would take the locations, links, tags
// and itinerary entries with it. This pins the rebuild in both directions.
//
// SQLite only: the dialect files differ here, but what needs testing is the
// rebuild, which only SQLite does.
func TestMigration0011AddsThreeCategoriesAndKeepsEverything(t *testing.T) {
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
	// each thing that hangs off it -- including an itinerary entry, which the
	// 0010 test talks about but never actually inserted.
	if err := newM().Migrate(10); err != nil {
		t.Fatalf("migrate to 10: %v", err)
	}
	exec := func(q string) {
		t.Helper()
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id, username, display_name, created_at, updated_at) VALUES ('u','u','U','2026-01-01','2026-01-01')`)
	exec(`INSERT INTO trips (id, owner_id, title, created_at, updated_at) VALUES ('t','u','T','2026-01-01','2026-01-01')`)
	exec(`INSERT INTO items (id, trip_id, category, title, sort_order, created_at, updated_at) VALUES ('i','t','area','Snaefellsnes',3,'2026-01-01','2026-01-01')`)
	exec(`INSERT INTO item_locations (id, item_id, lat, lng, address) VALUES ('l','i',64.9,-23.3,'Grundarfjordur')`)
	exec(`INSERT INTO item_links (id, item_id, url, sort_order) VALUES ('k','i','https://example.com',0)`)
	exec(`INSERT INTO item_tags (item_id, tag) VALUES ('i','landmark')`)
	exec(`INSERT INTO itinerary_days (id, trip_id, date) VALUES ('d','t','2026-06-01')`)
	exec(`INSERT INTO itinerary_entries (id, itinerary_day_id, item_id, sort_order) VALUES ('e','d','i',0)`)

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
	// The rebuild copies every column, not just the ones it is about -- and
	// the category value 0010 added is still there afterwards.
	if n := count(`SELECT count(*) FROM items WHERE id='i' AND title='Snaefellsnes' AND sort_order=3 AND category='area'`); n != 1 {
		t.Errorf("the item did not survive the rebuild intact (n=%d)", n)
	}
	for _, q := range []string{
		`SELECT count(*) FROM item_locations WHERE item_id='i'`,
		`SELECT count(*) FROM item_links WHERE item_id='i'`,
		`SELECT count(*) FROM item_tags WHERE item_id='i'`,
		`SELECT count(*) FROM itinerary_entries WHERE item_id='i'`,
	} {
		if n := count(q); n != 1 {
			t.Errorf("%s = %d, want 1 -- the drop cascaded", q, n)
		}
	}
	if n := count(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name='idx_items_trip_id_category'`); n != 1 {
		t.Errorf("idx_items_trip_id_category lost in the rebuild (n=%d)", n)
	}

	// The point of the whole thing: all three are legal now, and the
	// constraint still refuses anything else.
	for _, c := range []string{"food", "event", "shop"} {
		q := `INSERT INTO items (id, trip_id, category, title, created_at, updated_at) VALUES ('n` + c + `','t','` + c + `','N','2026-01-01','2026-01-01')`
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("%q rejected after 0011: %v", c, err)
		}
	}
	if _, err := conn.Exec(`INSERT INTO items (id, trip_id, category, title, created_at, updated_at) VALUES ('x','t','nonsense','X','2026-01-01','2026-01-01')`); err == nil {
		t.Error("the CHECK constraint is gone: a nonsense category was accepted")
	}

	// Down folds all three into sites rather than failing on them, leaves the
	// area alone, and cascades nothing away.
	if err := newM().Migrate(10); err != nil {
		t.Fatalf("down to 10: %v", err)
	}
	if n := count(`SELECT count(*) FROM items WHERE id IN ('nfood','nevent','nshop') AND category='site'`); n != 3 {
		t.Errorf("the three new categories were not folded back into sites (n=%d)", n)
	}
	if n := count(`SELECT count(*) FROM items WHERE id='i' AND category='area'`); n != 1 {
		t.Errorf("the area was disturbed by the down migration (n=%d)", n)
	}
	for _, q := range []string{
		`SELECT count(*) FROM item_tags WHERE item_id='i'`,
		`SELECT count(*) FROM itinerary_entries WHERE item_id='i'`,
	} {
		if n := count(q); n != 1 {
			t.Errorf("%s = %d, want 1 -- the down rebuild cascaded", q, n)
		}
	}
}
