package db

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Migration 0013 renames items to locations, and its satellites and every
// item_id column with it. The rows stay put, which a rename all but promises;
// what this pins is the part that is not obvious.
//
// SQLite rewrites the REFERENCES clauses of the other tables when a table is
// renamed, except with legacy_alter_table on and foreign_keys off. If it did
// not, files, itinerary_entries and expenses would still point at a table
// called items, which no longer exists, and the first delete would fail or
// cascade nowhere. So the test deletes the location and checks that every
// satellite went with it -- and that the expense, which is ON DELETE SET NULL,
// stayed and let go.
//
// SQLite only, like its neighbours. The Postgres side has no such question to
// answer: its foreign keys follow the table by OID.
//
// Like every migration test here, it migrates to its own version rather than to
// head, so a later migration cannot break it by renaming something again.
func TestMigration0013RenamesItemsToLocationsAndKeepsTheReferences(t *testing.T) {
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

	if err := newM().Migrate(12); err != nil {
		t.Fatalf("migrate to 12: %v", err)
	}
	exec := func(q string) {
		t.Helper()
		if _, err := conn.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	// One location with one of everything that hangs off it, under the old
	// names.
	exec(`INSERT INTO users (id, username, display_name, created_at, updated_at) VALUES ('u','u','U','2026-01-01','2026-01-01')`)
	exec(`INSERT INTO trips (id, owner_id, title, created_at, updated_at) VALUES ('t','u','T','2026-01-01','2026-01-01')`)
	exec(`INSERT INTO items (id, trip_id, category, title, created_at, updated_at) VALUES ('i','t','site','Kirkjufell','2026-01-01','2026-01-01')`)
	exec(`INSERT INTO item_locations (id, item_id, lat, lng, address) VALUES ('g','i',64.9,-23.3,'Grundarfjordur')`)
	exec(`INSERT INTO item_links (id, item_id, url, sort_order) VALUES ('k','i','https://example.com',0)`)
	exec(`INSERT INTO item_tags (item_id, tag) VALUES ('i','landmark')`)
	exec(`INSERT INTO files (id, trip_id, item_id, filename, storage_path, size_bytes, uploaded_at) VALUES ('f','t','i','a.pdf','x/a.pdf',1,'2026-01-01')`)
	exec(`INSERT INTO itinerary_days (id, trip_id, date) VALUES ('d','t','2026-06-01')`)
	exec(`INSERT INTO itinerary_entries (id, itinerary_day_id, item_id, sort_order) VALUES ('e','d','i',0)`)
	exec(`INSERT INTO expenses (id, trip_id, title, amount_minor, spent_on, item_id, created_at) VALUES ('x','t','Parking',500,'2026-06-01','i','2026-01-01')`)

	if err := newM().Migrate(13); err != nil {
		t.Fatalf("migrate to 13: %v", err)
	}

	count := func(q string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	// Every row is there under its new name.
	for _, q := range []string{
		`SELECT count(*) FROM locations WHERE id='i' AND title='Kirkjufell'`,
		`SELECT count(*) FROM location_geo WHERE location_id='i' AND address='Grundarfjordur'`,
		`SELECT count(*) FROM location_links WHERE location_id='i'`,
		`SELECT count(*) FROM location_tags WHERE location_id='i'`,
		`SELECT count(*) FROM files WHERE location_id='i'`,
		`SELECT count(*) FROM itinerary_entries WHERE location_id='i'`,
		`SELECT count(*) FROM expenses WHERE location_id='i'`,
	} {
		if n := count(q); n != 1 {
			t.Errorf("%s = %d, want 1", q, n)
		}
	}
	// Nothing in the schema still says item, apart from the checklist tables,
	// which are a different kind of item.
	if n := count(`SELECT count(*) FROM sqlite_master WHERE (name LIKE '%item%' OR sql LIKE '%item\_%' ESCAPE '\' OR sql LIKE '%items%') AND name NOT LIKE '%checklist%'`); n != 0 {
		t.Errorf("%d schema objects still name an item", n)
	}
	// The renamed indexes exist.
	for _, idx := range []string{
		"idx_locations_trip_id_category", "idx_location_links_location_id", "idx_location_tags_tag",
		"idx_files_location_id", "idx_itinerary_entries_location_id", "idx_expenses_location_id",
	} {
		if n := count(`SELECT count(*) FROM sqlite_master WHERE type='index' AND name='` + idx + `'`); n != 1 {
			t.Errorf("index %s missing", idx)
		}
	}

	// Down and up again, with the rows intact both ways.
	if err := newM().Migrate(12); err != nil {
		t.Fatalf("down to 12: %v", err)
	}
	if n := count(`SELECT count(*) FROM items i JOIN item_locations l ON l.item_id = i.id JOIN files f ON f.item_id = i.id`); n != 1 {
		t.Errorf("down lost the location or its references (n=%d)", n)
	}
	if err := newM().Migrate(13); err != nil {
		t.Fatalf("back up to 13: %v", err)
	}

	// The foreign keys followed the rename: deleting the location cascades into
	// every satellite and sets the expense loose.
	exec(`DELETE FROM locations WHERE id='i'`)
	for _, q := range []string{
		`SELECT count(*) FROM location_geo`,
		`SELECT count(*) FROM location_links`,
		`SELECT count(*) FROM location_tags`,
		`SELECT count(*) FROM files`,
		`SELECT count(*) FROM itinerary_entries`,
	} {
		if n := count(q); n != 0 {
			t.Errorf("%s = %d after deleting the location, want 0 -- the cascade did not follow the rename", q, n)
		}
	}
	if n := count(`SELECT count(*) FROM expenses WHERE id='x' AND location_id IS NULL`); n != 1 {
		t.Errorf("the expense did not survive with its location cleared (n=%d)", n)
	}
	if n := count(`SELECT count(*) FROM pragma_foreign_key_check`); n != 0 {
		t.Errorf("foreign_key_check reports %d violations", n)
	}
}
