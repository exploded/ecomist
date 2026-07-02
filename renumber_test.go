package main

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/exploded/ecomist/internal/db"
)

func TestRenumberDispensersContinuousWithRanges(t *testing.T) {
	d, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := migrate(d); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	// migrate() already seeds one franchise (id 1); reuse it.
	mustExec(t, d, `INSERT INTO dispenser_models (id, name) VALUES (1, 'Eco 6')`)
	mustExec(t, d, `INSERT INTO customers (id, franchise_id, name) VALUES (1, 1, 'Acme')`)
	// Zone 1 (sort 1) and Zone 2 (sort 2).
	mustExec(t, d, `INSERT INTO zones (id, customer_id, sort_order, label) VALUES (1, 1, 1, 'Zone 1')`)
	mustExec(t, d, `INSERT INTO zones (id, customer_id, sort_order, label) VALUES (2, 1, 2, 'Zone 2')`)

	seed := func(zoneID any, sortOrder, quantity int, loc string) {
		mustExec(t, d, `INSERT INTO dispensers (customer_id, zone_id, sort_order, location, model_id, quantity)
			VALUES (1, ?, ?, ?, 1, ?)`, zoneID, sortOrder, loc, quantity)
	}
	seed(1, 1, 1, "Foyer")    // expect "1"
	seed(1, 2, 2, "Corridor") // expect "2 - 3"  (qty 2 consumes two numbers)
	seed(2, 1, 1, "Kitchen")  // expect "4"  (count continues across the zone)
	seed(nil, 9, 1, "Loose")  // unzoned sorts last -> expect "5"

	q := db.New(d)
	if err := renumberDispensers(ctx, q, 1); err != nil {
		t.Fatal(err)
	}

	rows, err := q.ListDispensersByCustomer(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, row := range rows {
		got[row.Location] = row.SeqLabel
	}
	want := map[string]string{
		"Foyer":    "1",
		"Corridor": "2 - 3",
		"Kitchen":  "4",
		"Loose":    "5",
	}
	for loc, w := range want {
		if got[loc] != w {
			t.Errorf("%s: want seq_label %q, got %q", loc, w, got[loc])
		}
	}
}

// TestBackfillMigrationMatchesGo runs the 0004 backfill SQL against seeded
// stale labels and asserts it yields the same numbers as renumberDispensers.
func TestBackfillMigrationMatchesGo(t *testing.T) {
	d, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := migrate(d); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	mustExec(t, d, `INSERT INTO dispenser_models (id, name) VALUES (1, 'Eco 6')`)
	mustExec(t, d, `INSERT INTO customers (id, franchise_id, name) VALUES (1, 1, 'Acme')`)
	mustExec(t, d, `INSERT INTO zones (id, customer_id, sort_order, label) VALUES (1, 1, 1, 'Z1')`)
	mustExec(t, d, `INSERT INTO zones (id, customer_id, sort_order, label) VALUES (2, 1, 2, 'Z2')`)
	// Seed deliberately wrong seq_labels (as pre-change data would have).
	mustExec(t, d, `INSERT INTO dispensers (customer_id, zone_id, sort_order, location, model_id, quantity, seq_label) VALUES
		(1, 1, 1, 'Foyer', 1, 1, '7'),
		(1, 1, 2, 'Corridor', 1, 2, 'x'),
		(1, 2, 1, 'Kitchen', 1, 1, ''),
		(1, NULL, 9, 'Loose', 1, 1, '99')`)

	body, err := migrationFS.ReadFile("migrations/0004_renumber_dispensers.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(string(body)); err != nil {
		t.Fatalf("run backfill: %v", err)
	}

	q := db.New(d)
	rows, err := q.ListDispensersByCustomer(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"Foyer": "1", "Corridor": "2 - 3", "Kitchen": "4", "Loose": "5"}
	for _, row := range rows {
		if want[row.Location] != row.SeqLabel {
			t.Errorf("%s: want %q, got %q", row.Location, want[row.Location], row.SeqLabel)
		}
	}
}

func mustExec(t *testing.T, d *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := d.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}
