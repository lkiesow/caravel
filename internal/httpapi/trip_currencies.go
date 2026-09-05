package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"caravel/internal/db"
)

// The extra currencies a trip may record expenses in, alongside the single
// main currency in trips.currency. Stage 32.
//
// Shaped as a replace-all PUT rather than a collection of per-code resources,
// because that is what the settings form is: repeatable rows and one Save
// button. It also makes "remove this row" need no second verb, and makes two
// people saving the rates produce one set or the other rather than a mixture.
//
// The rate itself is explained where it is stored -- see db.TripCurrency and
// migration 0009. The short version: it converts minor units to minor units,
// so the server never has to know how many decimal places a currency has.

// maxRatePPB bounds a rate at a factor of one million, which is far past any
// real currency pair and still nowhere near overflowing the arithmetic. It
// exists so a typo cannot store a number that makes every total meaningless.
const maxRatePPB = int64(1_000_000) * db.RateOne

type tripCurrencyResponse struct {
	Code string `json:"code"`
	// RatePPB converts the minor unit of Code into the minor unit of the trip's
	// main currency, times a billion. The client folds the two currencies'
	// decimal exponents into this number before sending it and unfolds them
	// again to display it; see web/js/format.js.
	RatePPB int64 `json:"rate_ppb"`
}

type tripCurrenciesRequest struct {
	Currencies []tripCurrencyResponse `json:"currencies"`
}

type tripCurrenciesResponse struct {
	Currencies []tripCurrencyResponse `json:"currencies"`
}

// tripCurrencies loads a trip's additional currencies as response rows. Errors
// are the caller's to handle: unlike the member count on a trip response, this
// one cannot fail quietly. A dropped rate would not blank a label, it would
// silently reprice the whole ledger at 1:1.
func (s *Server) tripCurrencies(ctx context.Context, tripID string) ([]tripCurrencyResponse, error) {
	rows, err := s.Store.ListTripCurrencies(ctx, tripID)
	if err != nil {
		return nil, err
	}
	out := make([]tripCurrencyResponse, len(rows))
	for i, row := range rows {
		out[i] = tripCurrencyResponse{Code: row.Code, RatePPB: row.RatePPB}
	}
	return out, nil
}

// rateFor indexes currencies by code, with the trip's main currency mapping to
// the identity rate. This is the one place that decides what an expense's
// currency is worth, and both the conversion and the validation read it.
func rateIndex(mainCurrency string, currencies []tripCurrencyResponse) map[string]int64 {
	rates := map[string]int64{mainCurrency: db.RateOne}
	for _, c := range currencies {
		rates[c.Code] = c.RatePPB
	}
	return rates
}

// validate checks the body in isolation. The two rules that need the trip --
// that no code collides with the main currency, and that no code still in use
// is being dropped -- are checked by the handler, which has it.
func (req tripCurrenciesRequest) validate() error {
	seen := map[string]bool{}
	for _, c := range req.Currencies {
		// Checked against the allowlist rather than merely for shape, the same
		// as the main currency in tripRequest.validate: an unrecognised code
		// surfaces as a formatting failure screens away from the field.
		if !db.ValidCurrency(c.Code) {
			return fmt.Errorf("unsupported currency: %s", c.Code)
		}
		if seen[c.Code] {
			return fmt.Errorf("%s is listed twice", c.Code)
		}
		seen[c.Code] = true
		if c.RatePPB <= 0 {
			return fmt.Errorf("the exchange rate for %s must be greater than zero", c.Code)
		}
		if c.RatePPB > maxRatePPB {
			return fmt.Errorf("the exchange rate for %s is implausibly large", c.Code)
		}
	}
	return nil
}

func (s *Server) handleGetTripCurrencies(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleViewer)
	if !ok {
		return
	}
	currencies, err := s.tripCurrencies(r.Context(), trip.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the currencies")
		return
	}
	writeJSON(w, http.StatusOK, tripCurrenciesResponse{Currencies: currencies})
}

func (s *Server) handleSetTripCurrencies(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req tripCurrenciesRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The main currency is already the trip's own, at the identity rate. Listing
	// it here would ask for a second, contradictory answer to what a euro is
	// worth on a euro trip.
	for _, c := range req.Currencies {
		if c.Code == trip.Currency {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%s is already this trip's main currency", c.Code))
			return
		}
	}

	// Refuse to drop a currency expenses are still recorded in. The alternative
	// is worse in both directions: keeping the rate for a code no longer
	// configured hides it from every screen that could fix it, and dropping the
	// rate leaves amounts that cannot be converted at all.
	usage, err := s.Store.CountExpensesByCurrency(r.Context(), trip.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not check the currencies in use")
		return
	}
	keeping := map[string]bool{}
	for _, c := range req.Currencies {
		keeping[c.Code] = true
	}
	for _, in := range usage {
		if !keeping[in.Code] {
			writeError(w, http.StatusConflict, fmt.Sprintf(
				"%s cannot be removed: %d expense(s) are recorded in it", in.Code, in.ExpenseCount))
			return
		}
	}

	now := time.Now().UTC()
	err = s.Store.WithTx(r.Context(), func(store db.Store) error {
		if err := store.DeleteTripCurrenciesByTrip(r.Context(), trip.ID); err != nil {
			return err
		}
		for _, c := range req.Currencies {
			if _, err := store.CreateTripCurrency(r.Context(), db.CreateTripCurrencyParams{
				TripID:    trip.ID,
				Code:      c.Code,
				RatePPB:   c.RatePPB,
				CreatedAt: now,
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the currencies")
		return
	}

	// Read back rather than echoing the request: the response is then ordered
	// by code the way every later read will be, so a client that renders what
	// it gets does not reshuffle its rows on the next load.
	saved, err := s.tripCurrencies(r.Context(), trip.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the currencies")
		return
	}
	writeJSON(w, http.StatusOK, tripCurrenciesResponse{Currencies: saved})
}

// errMainCurrencyConfigured is what handleUpdateTrip reports when a trip tries
// to adopt a main currency it already lists as an additional one.
var errMainCurrencyConfigured = errors.New("that currency is already configured as an additional currency on this trip")
