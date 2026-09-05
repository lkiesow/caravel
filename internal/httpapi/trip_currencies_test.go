package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// HTTP-level coverage for the trip currency configuration added in Stage 32
// Milestone 2. The store-level round trips are in trip_currency_store_test.go;
// these cover the wiring and, mostly, the refusals -- which is where the value
// is, because every one of them exists to stop a ledger from quietly becoming
// wrong rather than to stop a malformed request.

// setCurrencies PUTs a currency set and returns the recorder, so a test can
// assert on either the success or the refusal.
func (ts *testServer) setCurrencies(cookie *http.Cookie, tripID, body string) *httptest.ResponseRecorder {
	ts.t.Helper()
	return ts.do(http.MethodPut, "/api/trips/"+tripID+"/currencies", cookie, body)
}

func TestTripCurrenciesRoundTripOverHTTP(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID := ts.createTrip(cookie, "Tokyo")

	// A fresh trip has none, and says so by omitting the field entirely --
	// which is what tells the client not to offer a picker.
	trip := decode[map[string]any](t, ts.do(http.MethodGet, "/api/trips/"+tripID, cookie, ""))
	if _, present := trip["currencies"]; present {
		t.Errorf("a trip with no extra currencies sent currencies: %v", trip["currencies"])
	}

	w := ts.setCurrencies(cookie, tripID,
		`{"currencies":[{"code":"USD","rate_ppb":920000000},{"code":"JPY","rate_ppb":580000000}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("put currencies: got %d, body %s", w.Code, w.Body.String())
	}

	// Read back from the PUT itself, ordered by code rather than as sent, so a
	// client rendering the response does not reshuffle on its next load.
	saved := decode[tripCurrenciesResponse](t, w)
	if len(saved.Currencies) != 2 {
		t.Fatalf("saved %d currencies, want 2", len(saved.Currencies))
	}
	if saved.Currencies[0].Code != "JPY" || saved.Currencies[1].Code != "USD" {
		t.Errorf("order: got %s then %s, want JPY then USD",
			saved.Currencies[0].Code, saved.Currencies[1].Code)
	}
	if saved.Currencies[0].RatePPB != 580000000 {
		t.Errorf("JPY rate: got %d, want 580000000", saved.Currencies[0].RatePPB)
	}

	// And on the trip itself, which is where the settings tab and the expense
	// form read it from.
	trip = decode[map[string]any](t, ts.do(http.MethodGet, "/api/trips/"+tripID, cookie, ""))
	listed, ok := trip["currencies"].([]any)
	if !ok || len(listed) != 2 {
		t.Fatalf("trip currencies: got %v, want 2 rows", trip["currencies"])
	}

	// The dedicated GET answers the same thing.
	got := decode[tripCurrenciesResponse](t, ts.do(http.MethodGet, "/api/trips/"+tripID+"/currencies", cookie, ""))
	if len(got.Currencies) != 2 {
		t.Errorf("GET currencies: got %d rows, want 2", len(got.Currencies))
	}
}

// The set is replaced wholesale, so a PUT naming fewer codes removes the rest.
func TestTripCurrenciesReplaceRemovesWhatIsNotNamed(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID := ts.createTrip(cookie, "Tokyo")

	if w := ts.setCurrencies(cookie, tripID,
		`{"currencies":[{"code":"USD","rate_ppb":920000000},{"code":"JPY","rate_ppb":580000000}]}`); w.Code != http.StatusOK {
		t.Fatalf("seed: got %d, body %s", w.Code, w.Body.String())
	}
	w := ts.setCurrencies(cookie, tripID, `{"currencies":[{"code":"JPY","rate_ppb":600000000}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("replace: got %d, body %s", w.Code, w.Body.String())
	}
	saved := decode[tripCurrenciesResponse](t, w)
	if len(saved.Currencies) != 1 || saved.Currencies[0].Code != "JPY" {
		t.Fatalf("after replace: %v, want only JPY", saved.Currencies)
	}
	if saved.Currencies[0].RatePPB != 600000000 {
		t.Errorf("rate after replace: got %d, want 600000000", saved.Currencies[0].RatePPB)
	}

	// An empty list is a legitimate save: it is how the last row is removed.
	w = ts.setCurrencies(cookie, tripID, `{"currencies":[]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("clear: got %d, body %s", w.Code, w.Body.String())
	}
	if got := decode[tripCurrenciesResponse](t, w).Currencies; len(got) != 0 {
		t.Errorf("after clearing: %v, want none", got)
	}
}

func TestTripCurrenciesRefusals(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	// A EUR trip, so EUR is the code that collides.
	tripID := ts.mustCreate(http.MethodPost, "/api/trips", cookie,
		`{"title":"Tokyo","currency":"EUR"}`, http.StatusCreated)

	cases := []struct {
		name string
		body string
	}{
		{"an unsupported code", `{"currencies":[{"code":"XYZ","rate_ppb":1000000000}]}`},
		{"a lowercase code", `{"currencies":[{"code":"jpy","rate_ppb":1000000000}]}`},
		{"the same code twice", `{"currencies":[{"code":"JPY","rate_ppb":1},{"code":"JPY","rate_ppb":2}]}`},
		{"a zero rate", `{"currencies":[{"code":"JPY","rate_ppb":0}]}`},
		{"a negative rate", `{"currencies":[{"code":"JPY","rate_ppb":-5}]}`},
		{"an implausible rate", `{"currencies":[{"code":"JPY","rate_ppb":9000000000000000}]}`},
		{"the trip's own main currency", `{"currencies":[{"code":"EUR","rate_ppb":1000000000}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := ts.setCurrencies(cookie, tripID, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("got %d, want 400 — body %s", w.Code, w.Body.String())
			}
			// The message has to name the problem, because it is rendered
			// verbatim next to the row that caused it.
			if w.Body.Len() == 0 {
				t.Error("refused with an empty body")
			}
		})
	}

	// A refused save must not have written anything on its way to refusing.
	if got := decode[tripCurrenciesResponse](t,
		ts.do(http.MethodGet, "/api/trips/"+tripID+"/currencies", cookie, "")).Currencies; len(got) != 0 {
		t.Errorf("a refused PUT left %v behind", got)
	}
}

// The guard that matters most: a currency expenses are recorded in cannot be
// removed, because both alternatives silently break the ledger.
func TestTripCurrencyInUseCannotBeRemoved(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID := ts.createTrip(cookie, "Tokyo")

	if w := ts.setCurrencies(cookie, tripID,
		`{"currencies":[{"code":"JPY","rate_ppb":580000000}]}`); w.Code != http.StatusOK {
		t.Fatalf("seed currencies: got %d, body %s", w.Code, w.Body.String())
	}
	expenseID := ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/expenses", cookie,
		`{"title":"Ramen","amount_minor":1200,"currency":"JPY","spent_on":"2026-08-19"}`, http.StatusCreated)

	w := ts.setCurrencies(cookie, tripID, `{"currencies":[]}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("removing a currency in use: got %d, want 409 — body %s", w.Code, w.Body.String())
	}
	// The message names the code and the count, so the person reading it knows
	// what to go and fix rather than only that they may not do this.
	if body := w.Body.String(); !strings.Contains(body, "JPY") || !strings.Contains(body, "1") {
		t.Errorf("refusal should name the code and the count, got %s", body)
	}

	// Editing the rate of a currency in use is fine -- that is the whole point
	// of a live rate.
	if w := ts.setCurrencies(cookie, tripID,
		`{"currencies":[{"code":"JPY","rate_ppb":600000000}]}`); w.Code != http.StatusOK {
		t.Fatalf("re-rating a currency in use: got %d, body %s", w.Code, w.Body.String())
	}

	// And once the expense is gone, so is the objection.
	if w := ts.do(http.MethodDelete, "/api/expenses/"+expenseID, cookie, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete expense: got %d", w.Code)
	}
	if w := ts.setCurrencies(cookie, tripID, `{"currencies":[]}`); w.Code != http.StatusOK {
		t.Fatalf("removing an unused currency: got %d, body %s", w.Code, w.Body.String())
	}
}

// A trip must not end up converting a currency to itself at a rate nobody
// chose.
func TestMainCurrencyCannotCollideWithAnAdditionalOne(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("owner")
	tripID := ts.mustCreate(http.MethodPost, "/api/trips", cookie,
		`{"title":"Tokyo","currency":"EUR"}`, http.StatusCreated)

	if w := ts.setCurrencies(cookie, tripID,
		`{"currencies":[{"code":"JPY","rate_ppb":580000000}]}`); w.Code != http.StatusOK {
		t.Fatalf("seed: got %d, body %s", w.Code, w.Body.String())
	}

	w := ts.do(http.MethodPatch, "/api/trips/"+tripID, cookie, `{"title":"Tokyo","currency":"JPY"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("adopting a configured currency as the main one: got %d, want 409 — body %s",
			w.Code, w.Body.String())
	}

	// An unrelated patch, and a patch to a currency that is *not* configured,
	// both still work -- the guard must not have made the field unwritable.
	if w := ts.do(http.MethodPatch, "/api/trips/"+tripID, cookie, `{"title":"Tokyo and Kyoto"}`); w.Code != http.StatusOK {
		t.Errorf("unrelated patch: got %d, body %s", w.Code, w.Body.String())
	}
	if w := ts.do(http.MethodPatch, "/api/trips/"+tripID, cookie, `{"title":"Tokyo","currency":"GBP"}`); w.Code != http.StatusOK {
		t.Errorf("patch to an unconfigured currency: got %d, body %s", w.Code, w.Body.String())
	}
}
