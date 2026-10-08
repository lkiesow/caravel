package db

import (
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Migration 0012 drops items.sort_order, and the rows it hangs off survive.
//
// A plain DROP COLUMN rather than the table rebuild 0010 and 0011 needed --
// there is no CHECK to alter and no index on the column -- so what this pins
// is mostly that the claim holds: SQLite really does drop it in place, the
// locations stay, and nothing cascades out of the tables that reference them.
//
// SQLite only, like its neighbours: DROP COLUMN on Postgres is not the part
// with anything to prove.
func TestMigration0012DropsSortOrderAndKeepsTheLocations(t *testing.T) {
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

	if err := newM().Migrate(11); err != nil {
		t.Fatalf("migrate to 11: %v", err)
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

	if err := newM().Migrate(12); err != nil {
		t.Fatalf("migrate to 12: %v", err)
	}

	count := func(q string) int {
		t.Helper()
		var n int
		if err := conn.QueryRow(q).Scan(&n); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		return n
	}
	if n := count(`SELECT count(*) FROM pragma_table_info('items') WHERE name='sort_order'`); n != 0 {
		t.Errorf("items.sort_order is still there (n=%d)", n)
	}
	if n := count(`SELECT count(*) FROM items WHERE id='i' AND title='Kirkjufell' AND category='site'`); n != 1 {
		t.Errorf("the location did not survive the drop (n=%d)", n)
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
	// item_links keeps its own sort_order: links are ordered by the array the
	// client sends, which is a real order somebody arranged.
	if n := count(`SELECT count(*) FROM pragma_table_info('item_links') WHERE name='sort_order'`); n != 1 {
		t.Errorf("item_links.sort_order was dropped too (n=%d)", n)
	}

	// Down brings the column back, defaulted, with the rows intact.
	if err := newM().Steps(-1); err != nil {
		t.Fatalf("down one: %v", err)
	}
	if n := count(`SELECT count(*) FROM items WHERE id='i' AND sort_order=0`); n != 1 {
		t.Errorf("the column did not come back defaulted (n=%d)", n)
	}
}
