package httpapi

import (
	"fmt"
	"net/http"
	"testing"
)

// Linking an expense to the location it was for (Stage 22 Milestone 3).
//
// The link is one-directional and optional: the expense points at a location so
// that "what was this, exactly" is one click from the picture and the notes.
// There is deliberately no per-location total, so nothing here asserts one.
//
// The load-bearing case is the last one: deleting the location must not delete
// the money. ON DELETE SET NULL is what makes that true, and a CASCADE typed by
// mistake would quietly change what a trip cost.

type locationExpenseFixture struct {
	ts         *testServer
	owner      *http.Cookie
	tripID     string
	locationID string
}

func setupLocationExpense(t *testing.T) *locationExpenseFixture {
	t.Helper()
	ts := newTestServer(t)
	owner := ts.login("owner")
	tripID := ts.createTrip(owner, "Iceland")
	return &locationExpenseFixture{ts: ts, owner: owner, tripID: tripID, locationID: ts.createLocation(owner, tripID, "Foss Hotel")}
}

func TestCreateExpenseWithALocation(t *testing.T) {
	f := setupLocationExpense(t)

	w := f.ts.do(http.MethodPost, "/api/trips/"+f.tripID+"/expenses", f.owner,
		fmt.Sprintf(`{"title":"Two nights","amount_minor":24000,"spent_on":"2026-08-20","location_id":%q}`, f.locationID))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d, body %s", w.Code, w.Body.String())
	}
	created := decode[struct {
		ID            string  `json:"id"`
		LocationID    *string `json:"location_id"`
		LocationTitle *string `json:"location_title"`
	}](t, w)
	if created.LocationID == nil || *created.LocationID != f.locationID {
		t.Errorf("created expense location_id is %v, want %s", created.LocationID, f.locationID)
	}
	// The title rides along so the client never has to load the trip's
	// locations to render one row.
	if created.LocationTitle == nil || *created.LocationTitle != "Foss Hotel" {
		t.Errorf("created expense location_title is %v, want Foss Hotel", created.LocationTitle)
	}

	// And it comes back on the listing, which is where it is actually read.
	list := f.ts.listExpenses(f.owner, f.tripID)
	if len(list.Expenses) != 1 {
		t.Fatalf("expected one expense, got %d", len(list.Expenses))
	}
	row := list.Expenses[0]
	if row.LocationID == nil || *row.LocationID != f.locationID || row.LocationTitle == nil || *row.LocationTitle != "Foss Hotel" {
		t.Errorf("listed row has location_id %v and location_title %v", row.LocationID, row.LocationTitle)
	}
}

// An expense with no location is the common case and must stay effortless: the
// field absent entirely, and null, both mean none.
func TestCreateExpenseWithoutALocation(t *testing.T) {
	f := setupLocationExpense(t)

	for name, body := range map[string]string{
		"field absent":  `{"title":"Groceries","amount_minor":1200,"spent_on":"2026-08-20"}`,
		"explicit null": `{"title":"Fuel","amount_minor":6500,"spent_on":"2026-08-20","location_id":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := f.ts.do(http.MethodPost, "/api/trips/"+f.tripID+"/expenses", f.owner, body)
			if w.Code != http.StatusCreated {
				t.Fatalf("create: got %d, body %s", w.Code, w.Body.String())
			}
			got := decode[struct {
				LocationID    *string `json:"location_id"`
				LocationTitle *string `json:"location_title"`
			}](t, w)
			if got.LocationID != nil || got.LocationTitle != nil {
				t.Errorf("got location_id %v and location_title %v, want both null", got.LocationID, got.LocationTitle)
			}
		})
	}
}

// PATCH replaces the whole expense, so an omitted location_id clears the link
// rather than leaving it -- the same rule the four original fields follow.
func TestUpdateExpenseSetsAndClearsTheLocation(t *testing.T) {
	f := setupLocationExpense(t)
	expenseID := f.ts.mustCreate(http.MethodPost, "/api/trips/"+f.tripID+"/expenses", f.owner,
		`{"title":"Two nights","amount_minor":24000,"spent_on":"2026-08-20"}`, http.StatusCreated)

	// On.
	w := f.ts.do(http.MethodPatch, "/api/expenses/"+expenseID, f.owner,
		fmt.Sprintf(`{"title":"Two nights","amount_minor":24000,"spent_on":"2026-08-20","location_id":%q}`, f.locationID))
	if w.Code != http.StatusOK {
		t.Fatalf("patch on: got %d, body %s", w.Code, w.Body.String())
	}
	if got := decode[struct {
		LocationID *string `json:"location_id"`
	}](t, w).LocationID; got == nil || *got != f.locationID {
		t.Fatalf("after setting, location_id is %v, want %s", got, f.locationID)
	}

	// Off, by omission.
	w = f.ts.do(http.MethodPatch, "/api/expenses/"+expenseID, f.owner,
		`{"title":"Two nights","amount_minor":24000,"spent_on":"2026-08-20"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch off: got %d, body %s", w.Code, w.Body.String())
	}
	if got := decode[struct {
		LocationID *string `json:"location_id"`
	}](t, w).LocationID; got != nil {
		t.Errorf("after omitting location_id it is %v, want null -- an expense is edited as a whole", got)
	}
}

// A location id from another trip must not be reachable through this one, and a
// nonexistent id is a 400 naming the field rather than a 500 from a foreign key.
func TestExpenseRejectsALocationFromAnotherTrip(t *testing.T) {
	f := setupLocationExpense(t)
	otherTrip := f.ts.createTrip(f.owner, "Norway")
	otherLocation := f.ts.createLocation(f.owner, otherTrip, "Someone else's hotel")

	cases := []struct {
		name       string
		locationID string
	}{
		{"another trip", otherLocation},
		{"no such location", "00000000-0000-0000-0000-000000000000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := f.ts.do(http.MethodPost, "/api/trips/"+f.tripID+"/expenses", f.owner,
				fmt.Sprintf(`{"title":"Ferry","amount_minor":4500,"spent_on":"2026-08-20","location_id":%q}`, tc.locationID))
			if w.Code != http.StatusBadRequest {
				t.Errorf("create: got %d, want 400 (body %s)", w.Code, w.Body.String())
			}
		})
	}

	// The same guard on update, which is the half easier to forget.
	expenseID := f.ts.mustCreate(http.MethodPost, "/api/trips/"+f.tripID+"/expenses", f.owner,
		`{"title":"Ferry","amount_minor":4500,"spent_on":"2026-08-20"}`, http.StatusCreated)
	w := f.ts.do(http.MethodPatch, "/api/expenses/"+expenseID, f.owner,
		fmt.Sprintf(`{"title":"Ferry","amount_minor":4500,"spent_on":"2026-08-20","location_id":%q}`, otherLocation))
	if w.Code != http.StatusBadRequest {
		t.Errorf("patch: got %d, want 400 (body %s)", w.Code, w.Body.String())
	}

	// Nothing was written by any of the four attempts.
	list := f.ts.listExpenses(f.owner, f.tripID)
	for _, row := range list.Expenses {
		if row.LocationID != nil {
			t.Errorf("expense %s ended up with location_id %v", row.Title, row.LocationID)
		}
	}
}

// The one that matters: deleting a location must not delete money. The expense
// survives with its amount intact and its link cleared.
func TestDeletingALocationKeepsItsExpenses(t *testing.T) {
	f := setupLocationExpense(t)
	f.ts.mustCreate(http.MethodPost, "/api/trips/"+f.tripID+"/expenses", f.owner,
		fmt.Sprintf(`{"title":"Two nights","amount_minor":24000,"spent_on":"2026-08-20","location_id":%q}`, f.locationID),
		http.StatusCreated)

	before := f.ts.listExpenses(f.owner, f.tripID)
	if before.TotalMinor != 24000 {
		t.Fatalf("total before the delete is %d, want 24000", before.TotalMinor)
	}

	w := f.ts.do(http.MethodDelete, "/api/locations/"+f.locationID, f.owner, "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete the location: got %d, body %s", w.Code, w.Body.String())
	}

	after := f.ts.listExpenses(f.owner, f.tripID)
	if len(after.Expenses) != 1 {
		t.Fatalf("expected the expense to survive, got %d rows", len(after.Expenses))
	}
	if after.TotalMinor != 24000 {
		t.Errorf("total is %d after deleting the location, want 24000 -- deleting a location must not change what the trip cost", after.TotalMinor)
	}
	if after.Expenses[0].LocationID != nil || after.Expenses[0].LocationTitle != nil {
		t.Errorf("surviving expense still points at the deleted location: location_id %v, location_title %v",
			after.Expenses[0].LocationID, after.Expenses[0].LocationTitle)
	}
}
