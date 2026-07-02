package main

import (
	"database/sql"
	"testing"

	"github.com/exploded/ecomist/internal/db"
)

func TestGroupDispensers(t *testing.T) {
	zones := []db.Zone{
		{ID: 1, Label: "ZONE 1"},
		{ID: 2, Label: "ZONE 2"},
	}
	dispensers := []db.ListDispensersByCustomerRow{
		{ID: 10, ZoneID: sqlNullInt(1)},
		{ID: 11, ZoneID: sqlNullInt(1)},
		{ID: 12, ZoneID: sqlNullInt(2)},
		{ID: 13},                         // no zone
		{ID: 14, ZoneID: sqlNullInt(99)}, // orphaned zone id -> no-zone bucket
	}
	groups := groupDispensers(zones, dispensers)
	if len(groups) != 3 {
		t.Fatalf("want 3 groups (2 zones + unzoned), got %d", len(groups))
	}
	if got := len(groups[0].Dispensers); got != 2 {
		t.Errorf("zone 1: want 2 dispensers, got %d", got)
	}
	if got := len(groups[1].Dispensers); got != 1 {
		t.Errorf("zone 2: want 1 dispenser, got %d", got)
	}
	if groups[2].Zone != nil || len(groups[2].Dispensers) != 2 {
		t.Errorf("unzoned bucket: want nil zone with 2 dispensers, got %+v", groups[2])
	}
}

func TestGroupDispensersEmptyZonesStillRender(t *testing.T) {
	zones := []db.Zone{{ID: 1, Label: "ZONE 1"}}
	groups := groupDispensers(zones, nil)
	if len(groups) != 1 || groups[0].Zone == nil {
		t.Fatalf("empty zone should still appear so units can be added to it: %+v", groups)
	}
}

func TestGroupStopDispensers(t *testing.T) {
	rows := []db.ListStopDispensersRow{
		{ID: 1, ZoneID: sqlNullInt(5), ZoneLabel: sql.NullString{String: "ZONE 1", Valid: true}},
		{ID: 2, ZoneID: sqlNullInt(5), ZoneLabel: sql.NullString{String: "ZONE 1", Valid: true}},
		{ID: 3, ZoneID: sqlNullInt(6), ZoneLabel: sql.NullString{String: "ZONE 2", Valid: true}},
		{ID: 4}, // unzoned
	}
	groups := groupStopDispensers(rows)
	if len(groups) != 3 {
		t.Fatalf("want 3 groups, got %d", len(groups))
	}
	if len(groups[0].Items) != 2 || groups[0].ZoneLabel != "ZONE 1" {
		t.Errorf("group 0 wrong: %+v", groups[0])
	}
	if len(groups[2].Items) != 1 || groups[2].ZoneID.Valid {
		t.Errorf("unzoned group wrong: %+v", groups[2])
	}
}

func TestSumTally(t *testing.T) {
	tally := []db.CustomerModelTallyRow{
		{ModelName: "Eco Maxi", Units: 16},
		{ModelName: "Eco Midi", Units: 15},
	}
	if got := sumTally(tally); got != 31 {
		t.Errorf("want 31 (the Waverley hospital check), got %d", got)
	}
}
