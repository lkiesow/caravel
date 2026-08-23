package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"caravel/internal/db"
)

// Behaviour of the expenses endpoints, at the HTTP level.
//
// The authorization matrix is not here: expenses were added to the table in
// roles_test.go, which already covers every trip-scoped route for all four
// kinds of caller, and to the requires-a-session list in ownership_test.go.
// Duplicating that here would be a second, weaker copy of the same policy.
// What this file covers is what those cannot: validation, the payer rules, and
// the totals.

// expenseList is the parsed GET /trips/{id}/expenses response.
type expenseList struct {
	Currency   string `json:"currency"`
	TotalMinor int64  `json:"total_minor"`
	Expenses   []struct {
		ID               string  `json:"id"`
		Title            string  `json:"title"`
		AmountMinor      int64   `json:"amount_minor"`
		SpentOn          string  `json:"spent_on"`
		PayerUserID      *string `json:"payer_user_id"`
		PayerDisplayName *string `json:"payer_display_name"`
	} `json:"expenses"`
}

func (ts *testServer) listExpenses(cookie *http.Cookie, tripID string) expenseList {
	ts.t.Helper()
	w := ts.do(http.MethodGet, "/api/trips/"+tripID+"/expenses", cookie, "")
	if w.Code != http.StatusOK {
		ts.t.Fatalf("list expenses: got %d, body %s", w.Code, w.Body.String())
	}
	return decode[expenseList](ts.t, w)
}

func TestCreateExpenseRejectsBadInput(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID := ts.createTrip(cookie, "Iceland")

	// Each of these is a distinct mistake, and the message has to name the
	// right one: a form that says "amount is required" when the date is
	// missing sends the user to the wrong field.
	for _, tc := range []struct{ name, body, wantMessage string }{
		{"no title", `{"amount_minor":100,"spent_on":"2026-08-20"}`, "title is required"},
		{"blank title", `{"title":"   ","amount_minor":100,"spent_on":"2026-08-20"}`, "title is required"},
		{"no amount", `{"title":"Bus","spent_on":"2026-08-20"}`, "amount is required"},
		{"zero amount", `{"title":"Bus","amount_minor":0,"spent_on":"2026-08-20"}`, "amount must be greater than zero"},
		{"negative amount", `{"title":"Bus","amount_minor":-500,"spent_on":"2026-08-20"}`, "amount must be greater than zero"},
		{"no date", `{"title":"Bus","amount_minor":100}`, "date is required"},
		{"malformed date", `{"title":"Bus","amount_minor":100,"spent_on":"20/08/2026"}`, "dates must be in YYYY-MM-DD format"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := ts.do(http.MethodPost, "/api/trips/"+tripID+"/expenses", cookie, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("got %d, want 400 — body %s", w.Code, w.Body.String())
			}
			if got := decode[map[string]string](t, w)["error"]; got != tc.wantMessage {
				t.Errorf("error: got %q, want %q", got, tc.wantMessage)
			}
		})
	}

	// Nothing was written by any of the refusals.
	if list := ts.listExpenses(cookie, tripID); len(list.Expenses) != 0 || list.TotalMinor != 0 {
		t.Errorf("a rejected create still wrote something: %+v", list)
	}
}

// The payer is whatever the request says, and the server never fills it in --
// see the note on expenseRequest for why the planned "absent means the caller"
// default was dropped. An omitted payer is unattributed, in both verbs.
func TestExpensePayerIsExactlyWhatTheRequestSays(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID := ts.createTrip(cookie, "Iceland")

	me, err := ts.Store.GetUserByUsername(context.Background(), "owner")
	if err != nil {
		t.Fatalf("look up owner: %v", err)
	}

	ts.mustCreate(
		http.MethodPost, "/api/trips/"+tripID+"/expenses", cookie,
		`{"title":"Ferry","amount_minor":1500,"spent_on":"2026-08-20","payer_user_id":"`+me.ID+`"}`,
		http.StatusCreated,
	)
	// Omitted, by the same caller, on the same trip: unattributed, not theirs.
	ts.mustCreate(
		http.MethodPost, "/api/trips/"+tripID+"/expenses", cookie,
		`{"title":"Parking","amount_minor":300,"spent_on":"2026-08-20"}`, http.StatusCreated,
	)

	list := ts.listExpenses(cookie, tripID)
	if len(list.Expenses) != 2 {
		t.Fatalf("got %d expenses, want 2", len(list.Expenses))
	}
	byTitle := map[string]*string{}
	names := map[string]*string{}
	for _, e := range list.Expenses {
		byTitle[e.Title] = e.PayerUserID
		names[e.Title] = e.PayerDisplayName
	}
	if got := byTitle["Ferry"]; got == nil || *got != me.ID {
		t.Errorf("explicit payer: got %v, want %q", got, me.ID)
	}
	if got := names["Ferry"]; got == nil || *got != "owner" {
		t.Errorf("payer_display_name: got %v, want %q", got, "owner")
	}
	if got := byTitle["Parking"]; got != nil {
		t.Errorf("omitted payer: got %q, want null", *got)
	}
}

// The payer id arrives in a request body, so no route param authorized it.
// Without the membership check any user id at all could be recorded as having
// paid for something on a trip they have nothing to do with.
func TestExpensePayerMustBeOnTheTrip(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	ts.login("outsider")
	tripID := ts.createTrip(owner, "Iceland")

	outsider, err := ts.Store.GetUserByUsername(context.Background(), "outsider")
	if err != nil {
		t.Fatalf("look up outsider: %v", err)
	}

	body := `{"title":"Ferry","amount_minor":1500,"spent_on":"2026-08-20","payer_user_id":"` + outsider.ID + `"}`
	w := ts.do(http.MethodPost, "/api/trips/"+tripID+"/expenses", owner, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create with an outsider as payer: got %d, want 400 — body %s", w.Code, w.Body.String())
	}

	// Now make them a member, and the same request is fine — proving the
	// refusal was about membership rather than about the field being rejected
	// outright.
	if _, err := ts.Store.UpsertTripMember(context.Background(), tripID, outsider.ID, db.RoleEditor, time.Now().UTC()); err != nil {
		t.Fatalf("add member: %v", err)
	}
	ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/expenses", owner, body, http.StatusCreated)

	// A member who paid and then left stays recorded: the row is not rewritten
	// by their departure, and the response still names them, which is why the
	// display name is resolved on the server rather than from the client's
	// member list.
	if _, err := ts.Store.DeleteTripMember(context.Background(), tripID, outsider.ID); err != nil {
		t.Fatalf("remove member: %v", err)
	}
	list := ts.listExpenses(owner, tripID)
	if len(list.Expenses) != 1 {
		t.Fatalf("got %d expenses, want 1", len(list.Expenses))
	}
	if got := list.Expenses[0].PayerDisplayName; got == nil || *got != "outsider" {
		t.Errorf("payer_display_name after they left the trip: got %v, want %q", got, "outsider")
	}
}

// An explicitly null payer is a legitimate value -- somebody outside the trip
// paid, or the payer's account is gone. This is the case that killed the
// planned "absent means the caller" default: Go cannot tell this body apart
// from one that omits the field, so defaulting would have made an unattributed
// expense impossible to record.
func TestExpenseAcceptsNullPayer(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID := ts.createTrip(cookie, "Iceland")

	ts.mustCreate(
		http.MethodPost, "/api/trips/"+tripID+"/expenses", cookie,
		`{"title":"Gift","amount_minor":900,"spent_on":"2026-08-20","payer_user_id":null}`, http.StatusCreated,
	)
	list := ts.listExpenses(cookie, tripID)
	if len(list.Expenses) != 1 {
		t.Fatalf("got %d expenses, want 1", len(list.Expenses))
	}
	if list.Expenses[0].PayerUserID != nil {
		t.Errorf("payer_user_id: got %q, want null", *list.Expenses[0].PayerUserID)
	}
	if list.Expenses[0].PayerDisplayName != nil {
		t.Errorf("payer_display_name: got %q, want null", *list.Expenses[0].PayerDisplayName)
	}
	// It still counts toward the total: an unattributed expense is money the
	// trip spent, and hiding it from the total would make the total wrong.
	if list.TotalMinor != 900 {
		t.Errorf("total_minor: got %d, want 900", list.TotalMinor)
	}
}

func TestListExpensesTotalAndOrder(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID := ts.createTrip(cookie, "Iceland")

	for _, e := range []string{
		`{"title":"Hostel","amount_minor":4500,"spent_on":"2026-08-18"}`,
		`{"title":"Dinner","amount_minor":3200,"spent_on":"2026-08-20"}`,
		`{"title":"Bus","amount_minor":750,"spent_on":"2026-08-19"}`,
	} {
		ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/expenses", cookie, e, http.StatusCreated)
	}

	list := ts.listExpenses(cookie, tripID)
	if list.TotalMinor != 8450 {
		t.Errorf("total_minor: got %d, want 8450", list.TotalMinor)
	}
	// The total is the database's answer, not a sum of the rows, so assert it
	// against the rows rather than trusting either alone.
	var summed int64
	for _, e := range list.Expenses {
		summed += e.AmountMinor
	}
	if summed != list.TotalMinor {
		t.Errorf("total_minor %d disagrees with the rows, which sum to %d", list.TotalMinor, summed)
	}
	if list.Currency != db.DefaultCurrency {
		t.Errorf("currency: got %q, want %q", list.Currency, db.DefaultCurrency)
	}
	for i, want := range []string{"Dinner", "Bus", "Hostel"} {
		if list.Expenses[i].Title != want {
			t.Errorf("expenses[%d]: got %q, want %q", i, list.Expenses[i].Title, want)
		}
	}
}

// An expense id is authorized through its own trip. Editing it needs a role on
// that trip, and holding one on a different trip is not it.
func TestUpdateExpenseFromAnotherTripIsNotFound(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID := ts.createTrip(cookie, "Iceland")
	otherTripID := ts.createTrip(cookie, "Norway")

	expenseID := ts.mustCreate(
		http.MethodPost, "/api/trips/"+tripID+"/expenses", cookie,
		`{"title":"Ferry","amount_minor":1500,"spent_on":"2026-08-20"}`, http.StatusCreated,
	)

	// The route carries no trip, so this is not a URL that can be wrong -- what
	// is under test is that the expense is authorized against the trip it
	// actually belongs to. An intruder with a trip of their own gets 404.
	intruder := ts.login("intruder")
	ts.createTrip(intruder, "Intruder's trip")
	w := ts.do(http.MethodPatch, "/api/expenses/"+expenseID, intruder,
		`{"title":"Hijacked","amount_minor":1,"spent_on":"2026-08-20"}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("patch someone else's expense: got %d, want 404 — body %s", w.Code, w.Body.String())
	}
	w = ts.do(http.MethodDelete, "/api/expenses/"+expenseID, intruder, "")
	if w.Code != http.StatusNotFound {
		t.Errorf("delete someone else's expense: got %d, want 404 — body %s", w.Code, w.Body.String())
	}

	// The owner's own copy is untouched.
	if list := ts.listExpenses(cookie, tripID); len(list.Expenses) != 1 || list.Expenses[0].Title != "Ferry" {
		t.Errorf("the expense was modified: %+v", list.Expenses)
	}
	if list := ts.listExpenses(cookie, otherTripID); len(list.Expenses) != 0 {
		t.Errorf("the other trip gained an expense: %+v", list.Expenses)
	}
}

func TestUpdateAndDeleteExpense(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID := ts.createTrip(cookie, "Iceland")

	expenseID := ts.mustCreate(
		http.MethodPost, "/api/trips/"+tripID+"/expenses", cookie,
		`{"title":"Ferry","amount_minor":1500,"spent_on":"2026-08-20"}`, http.StatusCreated,
	)

	w := ts.do(http.MethodPatch, "/api/expenses/"+expenseID, cookie,
		`{"title":"Ferry to Heimaey","amount_minor":1650,"spent_on":"2026-08-21"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch: got %d, body %s", w.Code, w.Body.String())
	}

	list := ts.listExpenses(cookie, tripID)
	if len(list.Expenses) != 1 {
		t.Fatalf("got %d expenses, want 1", len(list.Expenses))
	}
	got := list.Expenses[0]
	if got.Title != "Ferry to Heimaey" || got.AmountMinor != 1650 || got.SpentOn != "2026-08-21" {
		t.Errorf("after patch: %+v", got)
	}
	if list.TotalMinor != 1650 {
		t.Errorf("total_minor after patch: got %d, want 1650", list.TotalMinor)
	}
	// An absent payer means unattributed here too. A PATCH that defaulted to
	// whoever sent it would silently reassign somebody else's expense to the
	// person who fixed a typo in its title.
	if got.PayerUserID != nil {
		t.Errorf("payer_user_id after a patch that omitted it: got %q, want null", *got.PayerUserID)
	}

	w = ts.do(http.MethodDelete, "/api/expenses/"+expenseID, cookie, "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: got %d, body %s", w.Code, w.Body.String())
	}
	if list := ts.listExpenses(cookie, tripID); len(list.Expenses) != 0 || list.TotalMinor != 0 {
		t.Errorf("after delete: %+v", list)
	}
}

// A viewer may read the ledger. This is the one read in the matrix that is
// worth asserting the *content* of too: "not 403" would pass on an endpoint
// that returned an empty list to everyone.
func TestViewerSeesTheLedger(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	viewer := ts.login("viewer")
	tripID := ts.createTrip(owner, "Iceland")
	ts.mustCreate(
		http.MethodPost, "/api/trips/"+tripID+"/expenses", owner,
		`{"title":"Ferry","amount_minor":1500,"spent_on":"2026-08-20"}`, http.StatusCreated,
	)

	user, err := ts.Store.GetUserByUsername(context.Background(), "viewer")
	if err != nil {
		t.Fatalf("look up viewer: %v", err)
	}
	if _, err := ts.Store.UpsertTripMember(context.Background(), tripID, user.ID, db.RoleViewer, time.Now().UTC()); err != nil {
		t.Fatalf("add viewer: %v", err)
	}

	list := ts.listExpenses(viewer, tripID)
	if len(list.Expenses) != 1 || list.Expenses[0].Title != "Ferry" || list.TotalMinor != 1500 {
		t.Errorf("a viewer should see the whole ledger, got %+v", list)
	}
}

func TestTripCurrencyThroughTheAPI(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")

	// Created without a currency: the shipped default.
	tripID := ts.createTrip(cookie, "Iceland")
	if got := ts.listExpenses(cookie, tripID).Currency; got != db.DefaultCurrency {
		t.Errorf("default currency: got %q, want %q", got, db.DefaultCurrency)
	}

	// Created with one.
	yenTripID := ts.mustCreate(http.MethodPost, "/api/trips", cookie,
		`{"title":"Tokyo","currency":"JPY"}`, http.StatusCreated)
	if got := ts.listExpenses(cookie, yenTripID).Currency; got != "JPY" {
		t.Errorf("currency at create: got %q, want JPY", got)
	}

	// An update that says nothing about currency must leave it alone rather
	// than resetting a trip priced in yen back to the default.
	w := ts.do(http.MethodPatch, "/api/trips/"+yenTripID, cookie, `{"title":"Tokyo and Kyoto"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch trip: got %d, body %s", w.Code, w.Body.String())
	}
	if got := decode[map[string]any](t, w)["currency"]; got != "JPY" {
		t.Errorf("currency after an unrelated patch: got %v, want JPY", got)
	}

	// And an update that does name one changes it.
	w = ts.do(http.MethodPatch, "/api/trips/"+yenTripID, cookie, `{"title":"Tokyo","currency":"USD"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch currency: got %d, body %s", w.Code, w.Body.String())
	}
	if got := ts.listExpenses(cookie, yenTripID).Currency; got != "USD" {
		t.Errorf("currency after patch: got %q, want USD", got)
	}

	// An unrecognised code is refused where the mistake was made, rather than
	// surfacing later as a formatting failure in the expenses tab.
	for _, bad := range []string{`"XYZ"`, `"eur"`, `""`} {
		w := ts.do(http.MethodPatch, "/api/trips/"+tripID, cookie, `{"title":"Iceland","currency":`+bad+`}`)
		if w.Code != http.StatusBadRequest {
			t.Errorf("currency %s: got %d, want 400 — body %s", bad, w.Code, w.Body.String())
		}
	}
	w = ts.do(http.MethodPost, "/api/trips", cookie, `{"title":"Bad","currency":"XYZ"}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("create with an unsupported currency: got %d, want 400", w.Code)
	}
}
