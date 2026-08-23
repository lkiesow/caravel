package httpapi

import (
	"context"
	"net/http"
	"slices"
	"strconv"
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
		ID               string   `json:"id"`
		Title            string   `json:"title"`
		AmountMinor      int64    `json:"amount_minor"`
		SpentOn          string   `json:"spent_on"`
		PayerUserID      *string  `json:"payer_user_id"`
		PayerDisplayName *string  `json:"payer_display_name"`
		ShareUserIDs     []string `json:"share_user_ids"`
		ShareMinor       *int64   `json:"share_minor"`
	} `json:"expenses"`
	Balances struct {
		People []struct {
			UserID      string  `json:"user_id"`
			DisplayName *string `json:"display_name"`
			PaidMinor   int64   `json:"paid_minor"`
			OwedMinor   int64   `json:"owed_minor"`
			NetMinor    int64   `json:"net_minor"`
		} `json:"people"`
		Transfers []struct {
			FromUserID  string `json:"from_user_id"`
			ToUserID    string `json:"to_user_id"`
			AmountMinor int64  `json:"amount_minor"`
		} `json:"transfers"`
		UnattributedMinor int64 `json:"unattributed_minor"`
	} `json:"balances"`
	Payers []struct {
		UserID      *string `json:"user_id"`
		DisplayName *string `json:"display_name"`
		PaidMinor   int64   `json:"paid_minor"`
	} `json:"payers"`
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

// Per-person totals, which the server owns for the same reason it owns
// total_minor: an aggregate should have one implementation. Milestone 6's
// balances are this grouping with a division on top, so two places deciding
// what somebody paid is how a ledger and a balance come to disagree.
func TestPerPersonTotals(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	ts.login("bram")
	ts.login("cleo")
	tripID := ts.createTrip(owner, "Iceland")

	ids := map[string]string{}
	for _, name := range []string{"owner", "bram", "cleo"} {
		user, err := ts.Store.GetUserByUsername(context.Background(), name)
		if err != nil {
			t.Fatalf("look up %s: %v", name, err)
		}
		ids[name] = user.ID
		if name != "owner" {
			if _, err := ts.Store.UpsertTripMember(context.Background(), tripID, user.ID, db.RoleEditor, time.Now().UTC()); err != nil {
				t.Fatalf("add %s: %v", name, err)
			}
		}
	}

	pay := func(title string, amount int64, payer string) {
		t.Helper()
		body := `{"title":"` + title + `","amount_minor":` + strconv.FormatInt(amount, 10) + `,"spent_on":"2026-08-20"`
		if payer == "" {
			body += `,"payer_user_id":null}`
		} else {
			body += `,"payer_user_id":"` + ids[payer] + `"}`
		}
		ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/expenses", owner, body, http.StatusCreated)
	}

	// Two expenses for one person, to prove they are summed rather than listed
	// twice, and an unattributed pair, to prove they collapse into one row.
	pay("Hostel", 4000, "bram")
	pay("Dinner", 2000, "bram")
	pay("Bus", 5000, "owner")
	pay("Parking", 300, "")
	pay("Tip", 200, "")

	list := ts.listExpenses(owner, tripID)
	if list.TotalMinor != 11500 {
		t.Errorf("total_minor: got %d, want 11500", list.TotalMinor)
	}
	if len(list.Payers) != 3 {
		t.Fatalf("got %d payer rows, want 3 (bram, owner, unattributed) -- %+v", len(list.Payers), list.Payers)
	}

	// Most paid first: bram 6000, owner 5000, unattributed 500.
	wantOrder := []struct {
		name string
		paid int64
	}{{"bram", 6000}, {"owner", 5000}, {"", 500}}
	for i, want := range wantOrder {
		got := list.Payers[i]
		if want.name == "" {
			if got.UserID != nil || got.DisplayName != nil {
				t.Errorf("payers[%d]: expected the unattributed row, got %v/%v", i, got.UserID, got.DisplayName)
			}
		} else {
			if got.DisplayName == nil || *got.DisplayName != want.name {
				t.Errorf("payers[%d]: name got %v, want %q", i, got.DisplayName, want.name)
			}
			if got.UserID == nil || *got.UserID != ids[want.name] {
				t.Errorf("payers[%d]: id got %v, want %q", i, got.UserID, ids[want.name])
			}
		}
		if got.PaidMinor != want.paid {
			t.Errorf("payers[%d] (%s): paid got %d, want %d", i, want.name, got.PaidMinor, want.paid)
		}
	}

	// The rows must account for every expense: a grouping that drops one is a
	// summary that quietly disagrees with the ledger above it.
	var summed int64
	for _, p := range list.Payers {
		summed += p.PaidMinor
	}
	if summed != list.TotalMinor {
		t.Errorf("payer rows sum to %d, but the trip total is %d", summed, list.TotalMinor)
	}

	// Somebody on the trip who has paid nothing is absent by design: this
	// answers "who paid", and a row of zero answers nothing. Cleo is a member
	// and has paid for nothing.
	for _, p := range list.Payers {
		if p.UserID != nil && *p.UserID == ids["cleo"] {
			t.Errorf("cleo has paid nothing and should not appear in payers: %+v", p)
		}
	}
}

// A tie must not reshuffle between reloads: without a deterministic
// tie-breaker the summary reorders itself for two people who paid the same.
func TestPerPersonTotalsAreStableOnATie(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	ts.login("zoe")
	ts.login("adam")
	tripID := ts.createTrip(owner, "Iceland")

	for _, name := range []string{"zoe", "adam"} {
		user, err := ts.Store.GetUserByUsername(context.Background(), name)
		if err != nil {
			t.Fatalf("look up %s: %v", name, err)
		}
		if _, err := ts.Store.UpsertTripMember(context.Background(), tripID, user.ID, db.RoleEditor, time.Now().UTC()); err != nil {
			t.Fatalf("add %s: %v", name, err)
		}
		// zoe first, adam second, so insertion order and alphabetical order
		// disagree -- otherwise the test would pass on either rule.
		ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/expenses", owner,
			`{"title":"`+name+`s round","amount_minor":1000,"spent_on":"2026-08-20","payer_user_id":"`+user.ID+`"}`,
			http.StatusCreated)
	}

	for i := 0; i < 3; i++ {
		list := ts.listExpenses(owner, tripID)
		if len(list.Payers) != 2 {
			t.Fatalf("got %d payer rows, want 2", len(list.Payers))
		}
		if got := *list.Payers[0].DisplayName; got != "adam" {
			t.Errorf("read %d: first payer on a tie is %q, want the alphabetically first (adam)", i, got)
		}
	}
}

// tripWithThree returns a trip owned by `owner` plus two editors, and the three
// user ids by username. The shape most share tests need.
func tripWithThree(t *testing.T, ts *testServer, owner *http.Cookie) (tripID string, ids map[string]string) {
	t.Helper()
	tripID = ts.createTrip(owner, "Iceland")
	ids = map[string]string{}
	for _, name := range []string{"owner", "bram", "cleo"} {
		user, err := ts.Store.GetUserByUsername(context.Background(), name)
		if err != nil {
			t.Fatalf("look up %s: %v", name, err)
		}
		ids[name] = user.ID
		if name != "owner" {
			if _, err := ts.Store.UpsertTripMember(context.Background(), tripID, user.ID, db.RoleEditor, time.Now().UTC()); err != nil {
				t.Fatalf("add %s: %v", name, err)
			}
		}
	}
	return tripID, ids
}

// An expense naming no shares is for everyone on the trip, and the response
// says so explicitly rather than sending an empty list for the client to
// interpret. The rule lives in one place, on the server.
func TestExpenseWithNoSharesIsForEveryone(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	ts.login("bram")
	ts.login("cleo")
	tripID, ids := tripWithThree(t, ts, owner)

	ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/expenses", owner,
		`{"title":"Petrol","amount_minor":1000,"spent_on":"2026-08-20","payer_user_id":"`+ids["owner"]+`"}`,
		http.StatusCreated)

	list := ts.listExpenses(owner, tripID)
	got := list.Expenses[0]
	if len(got.ShareUserIDs) != 3 {
		t.Fatalf("share_user_ids: got %v, want all three participants", got.ShareUserIDs)
	}
	for _, name := range []string{"owner", "bram", "cleo"} {
		if !slices.Contains(got.ShareUserIDs, ids[name]) {
			t.Errorf("share_user_ids is missing %s", name)
		}
	}
	// 1000 across three, and the reader is whoever asked.
	if got.ShareMinor == nil {
		t.Fatal("share_minor: got nil, want the reader's own share")
	}
	if *got.ShareMinor != 334 && *got.ShareMinor != 333 {
		t.Errorf("share_minor: got %d, want 334 or 333", *got.ShareMinor)
	}
}

// The retroactive half of that rule, which the migration comment calls out: a
// member added later shares in expenses recorded before they arrived, because
// nothing was written down at the time.
func TestNewMemberSharesPastExpenses(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	ts.login("bram")
	tripID := ts.createTrip(owner, "Iceland")

	ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/expenses", owner,
		`{"title":"Ferry","amount_minor":1000,"spent_on":"2026-08-20"}`, http.StatusCreated)
	if got := ts.listExpenses(owner, tripID).Expenses[0]; len(got.ShareUserIDs) != 1 {
		t.Fatalf("on a solo trip the share set should be just the owner, got %v", got.ShareUserIDs)
	}

	bram, err := ts.Store.GetUserByUsername(context.Background(), "bram")
	if err != nil {
		t.Fatalf("look up bram: %v", err)
	}
	if _, err := ts.Store.UpsertTripMember(context.Background(), tripID, bram.ID, db.RoleEditor, time.Now().UTC()); err != nil {
		t.Fatalf("add bram: %v", err)
	}

	got := ts.listExpenses(owner, tripID).Expenses[0]
	if len(got.ShareUserIDs) != 2 {
		t.Errorf("after adding a member the share set should be both, got %v", got.ShareUserIDs)
	}
	if got.ShareMinor == nil || *got.ShareMinor != 500 {
		t.Errorf("share_minor: got %v, want 500 once the expense is split two ways", got.ShareMinor)
	}
}

// Naming a subset pins the expense to those people.
func TestExpenseSharedWithASubset(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	ts.login("bram")
	ts.login("cleo")
	tripID, ids := tripWithThree(t, ts, owner)

	ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/expenses", owner,
		`{"title":"Two tickets","amount_minor":1000,"spent_on":"2026-08-20","payer_user_id":"`+ids["owner"]+`",`+
			`"share_user_ids":["`+ids["owner"]+`","`+ids["bram"]+`"]}`, http.StatusCreated)

	got := ts.listExpenses(owner, tripID).Expenses[0]
	if len(got.ShareUserIDs) != 2 || slices.Contains(got.ShareUserIDs, ids["cleo"]) {
		t.Errorf("share_user_ids: got %v, want owner and bram only", got.ShareUserIDs)
	}
	if got.ShareMinor == nil || *got.ShareMinor != 500 {
		t.Errorf("share_minor for the owner: got %v, want 500", got.ShareMinor)
	}

	// Somebody outside the share set has no share of it at all, which is
	// different from having a share of zero.
	cleo := ts.session(ids["cleo"])
	fromCleo := ts.listExpenses(cleo, tripID).Expenses[0]
	if fromCleo.ShareMinor != nil {
		t.Errorf("cleo is not sharing this expense, so share_minor should be null, got %d", *fromCleo.ShareMinor)
	}
}

// A set naming everybody is stored as no rows, so it keeps meaning "everyone"
// when the trip grows. Otherwise saving an unrelated edit would silently pin an
// expense that was never pinned, with nothing to tell the client it happened.
func TestSharingWithEveryoneStaysImplicit(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	ts.login("bram")
	ts.login("cleo")
	tripID, ids := tripWithThree(t, ts, owner)

	expenseID := ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/expenses", owner,
		`{"title":"Fuel","amount_minor":900,"spent_on":"2026-08-20","share_user_ids":["`+
			ids["owner"]+`","`+ids["bram"]+`","`+ids["cleo"]+`"]}`, http.StatusCreated)

	stored, err := ts.Store.ListExpenseShareUsers(context.Background(), expenseID)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	if len(stored) != 0 {
		t.Errorf("naming everybody should store no rows, got %v", stored)
	}

	// And it behaves as everyone: a fourth person joins and is included.
	ts.login("dana")
	dana, err := ts.Store.GetUserByUsername(context.Background(), "dana")
	if err != nil {
		t.Fatalf("look up dana: %v", err)
	}
	if _, err := ts.Store.UpsertTripMember(context.Background(), tripID, dana.ID, db.RoleEditor, time.Now().UTC()); err != nil {
		t.Fatalf("add dana: %v", err)
	}
	if got := ts.listExpenses(owner, tripID).Expenses[0]; len(got.ShareUserIDs) != 4 {
		t.Errorf("share set after a fourth member joined: got %v, want all four", got.ShareUserIDs)
	}
}

func TestExpenseShareMustBeOnTheTrip(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	ts.login("outsider")
	tripID := ts.createTrip(owner, "Iceland")

	outsider, err := ts.Store.GetUserByUsername(context.Background(), "outsider")
	if err != nil {
		t.Fatalf("look up outsider: %v", err)
	}
	w := ts.do(http.MethodPost, "/api/trips/"+tripID+"/expenses", owner,
		`{"title":"Ferry","amount_minor":1000,"spent_on":"2026-08-20","share_user_ids":["`+outsider.ID+`"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("sharing with an outsider: got %d, want 400 -- body %s", w.Code, w.Body.String())
	}
	// The refusal wrote nothing: the expense and its shares are one
	// transaction, so a rejected share set cannot leave the expense behind.
	if list := ts.listExpenses(owner, tripID); len(list.Expenses) != 0 {
		t.Errorf("a refused share set still created the expense: %+v", list.Expenses)
	}
}

// Updating replaces the whole set rather than adding to it: the request states
// who the expense is for, so anybody it does not name is no longer among them.
func TestUpdateReplacesTheShareSet(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	ts.login("bram")
	ts.login("cleo")
	tripID, ids := tripWithThree(t, ts, owner)

	expenseID := ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/expenses", owner,
		`{"title":"Dinner","amount_minor":900,"spent_on":"2026-08-20","share_user_ids":["`+
			ids["owner"]+`","`+ids["bram"]+`"]}`, http.StatusCreated)

	w := ts.do(http.MethodPatch, "/api/expenses/"+expenseID, owner,
		`{"title":"Dinner","amount_minor":900,"spent_on":"2026-08-20","share_user_ids":["`+ids["cleo"]+`"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch: got %d, body %s", w.Code, w.Body.String())
	}
	stored, err := ts.Store.ListExpenseShareUsers(context.Background(), expenseID)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	if len(stored) != 1 || stored[0] != ids["cleo"] {
		t.Errorf("share set after the patch: got %v, want cleo only", stored)
	}

	// And back to everyone, by sending an empty set.
	w = ts.do(http.MethodPatch, "/api/expenses/"+expenseID, owner,
		`{"title":"Dinner","amount_minor":900,"spent_on":"2026-08-20","share_user_ids":[]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch back to everyone: got %d, body %s", w.Code, w.Body.String())
	}
	stored, err = ts.Store.ListExpenseShareUsers(context.Background(), expenseID)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	if len(stored) != 0 {
		t.Errorf("an empty set should store no rows, got %v", stored)
	}
}

// A duplicated id is redundant rather than wrong, so it is ignored -- but it
// must not double that person's weight in the split, which is what would happen
// if it reached splitAmount.
func TestDuplicateShareIDsDoNotDoubleAShare(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	ts.login("bram")
	ts.login("cleo")
	tripID, ids := tripWithThree(t, ts, owner)

	ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/expenses", owner,
		`{"title":"Taxi","amount_minor":1000,"spent_on":"2026-08-20","share_user_ids":["`+
			ids["owner"]+`","`+ids["owner"]+`","`+ids["bram"]+`"]}`, http.StatusCreated)

	got := ts.listExpenses(owner, tripID).Expenses[0]
	if len(got.ShareUserIDs) != 2 {
		t.Errorf("share_user_ids: got %v, want two distinct people", got.ShareUserIDs)
	}
	if got.ShareMinor == nil || *got.ShareMinor != 500 {
		t.Errorf("share_minor: got %v, want 500 -- a repeated id must not weight the split", got.ShareMinor)
	}
}

func TestDeletingAnExpenseDeletesItsShares(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	ts.login("bram")
	ts.login("cleo")
	tripID, ids := tripWithThree(t, ts, owner)

	expenseID := ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/expenses", owner,
		`{"title":"Museum","amount_minor":600,"spent_on":"2026-08-20","share_user_ids":["`+ids["bram"]+`"]}`,
		http.StatusCreated)
	if w := ts.do(http.MethodDelete, "/api/expenses/"+expenseID, owner, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete: got %d", w.Code)
	}
	stored, err := ts.Store.ListExpenseShareUsers(context.Background(), expenseID)
	if err != nil {
		t.Fatalf("list shares: %v", err)
	}
	if len(stored) != 0 {
		t.Errorf("shares survived their expense: %v", stored)
	}
}

// The balances reach the client, over the real handler, with the real share
// resolution behind them. The arithmetic itself is covered in
// expense_balances_test.go; this is about the wiring.
func TestBalancesOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	ts.login("bram")
	ts.login("cleo")
	tripID, ids := tripWithThree(t, ts, owner)

	// Owner pays 1000 for everyone, and an unattributed 300 nobody is credited
	// for. Cleo pays nothing.
	ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/expenses", owner,
		`{"title":"Petrol","amount_minor":1000,"spent_on":"2026-08-20","payer_user_id":"`+ids["owner"]+`"}`,
		http.StatusCreated)
	ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/expenses", owner,
		`{"title":"Parking","amount_minor":300,"spent_on":"2026-08-20","payer_user_id":null}`,
		http.StatusCreated)

	list := ts.listExpenses(owner, tripID)
	b := list.Balances

	if b.UnattributedMinor != 300 {
		t.Errorf("unattributed_minor: got %d, want 300", b.UnattributedMinor)
	}
	if len(b.People) != 3 {
		t.Fatalf("want a row for all three participants, got %+v", b.People)
	}

	var netTotal int64
	byID := map[string]int64{}
	for _, p := range b.People {
		netTotal += p.NetMinor
		byID[p.UserID] = p.NetMinor
		if p.DisplayName == nil {
			t.Errorf("balance row for %s has no display name", p.UserID)
		}
	}
	if netTotal != 0 {
		t.Errorf("nets sum to %d, want 0 -- the unattributed 300 must not reach anybody", netTotal)
	}
	// 1000 across three: the owner is owed 1000 minus their own 334 or 333.
	if byID[ids["owner"]] <= 0 {
		t.Errorf("the owner paid for everyone and should be owed money, got %d", byID[ids["owner"]])
	}
	for _, name := range []string{"bram", "cleo"} {
		if byID[ids[name]] >= 0 {
			t.Errorf("%s paid nothing and should owe money, got %d", name, byID[ids[name]])
		}
	}

	// Following the transfers settles everybody.
	remaining := map[string]int64{}
	for _, p := range b.People {
		remaining[p.UserID] = p.NetMinor
	}
	for _, tr := range b.Transfers {
		remaining[tr.FromUserID] += tr.AmountMinor
		remaining[tr.ToUserID] -= tr.AmountMinor
	}
	for id, left := range remaining {
		if left != 0 {
			t.Errorf("%s is %d from settled after the suggested transfers", id, left)
		}
	}
}

// A solo trip has nothing to balance, and must not claim otherwise.
func TestBalancesOnASoloTrip(t *testing.T) {
	ts := newTestServer(t)
	owner := ts.login("owner")
	tripID := ts.createTrip(owner, "Iceland")
	ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/expenses", owner,
		`{"title":"Coffee","amount_minor":420,"spent_on":"2026-08-20"}`, http.StatusCreated)

	b := ts.listExpenses(owner, tripID).Balances
	if len(b.People) != 1 || b.People[0].NetMinor != 0 {
		t.Errorf("a solo trip balances to zero, got %+v", b.People)
	}
	if len(b.Transfers) != 0 {
		t.Errorf("nothing to settle on a solo trip, got %+v", b.Transfers)
	}
}
