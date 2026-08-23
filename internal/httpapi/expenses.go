package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"caravel/internal/db"
)

// Expenses. Stage 17.
//
// The shape follows checklists — a trip-scoped collection, listed and created
// under /trips/{tripId}, mutated under /expenses/{expenseId} — with one
// structural difference: there is no visibility axis, so nothing here filters
// by the reading user and loadExpense has no predicate after its authorization.
// Every expense on a trip is visible to everyone on it, deliberately, because
// hidden rows in a shared ledger make an incorrect total look correct.

type expenseResponse struct {
	ID     string `json:"id"`
	TripID string `json:"trip_id"`
	Title  string `json:"title"`
	// AmountMinor is an integer in the trip's currency's minor unit, never a
	// formatted string: formatting is the client's job, and the exponent
	// differs per currency (see web/js/format.js, which reads it from Intl).
	AmountMinor int64  `json:"amount_minor"`
	SpentOn     string `json:"spent_on"`
	// PayerUserID is null when the payer's account has been deleted. The row
	// stays visible and still counts toward the total; it is the balances view
	// that has to say it cannot attribute it.
	PayerUserID *string `json:"payer_user_id"`
	// PayerDisplayName saves the client resolving the id itself. It cannot:
	// somebody who paid and later left the trip is not in the members list, so
	// a client-side lookup would render a blank name for a payer who is still
	// perfectly well recorded.
	PayerDisplayName *string `json:"payer_display_name"`
	CreatedAt        string  `json:"created_at"`
}

// expenseListResponse carries the currency and the total alongside the rows.
// Both are on the envelope rather than left to the client: the total is summed
// by the database, so it stays right even for a client showing part of the
// list, and a currency sent explicitly is one the client never has to infer.
type expenseListResponse struct {
	Currency   string            `json:"currency"`
	TotalMinor int64             `json:"total_minor"`
	Expenses   []expenseResponse `json:"expenses"`
}

// payerNamer resolves payer ids to display names for one response, caching so
// a list of twenty expenses paid by two people costs two lookups rather than
// twenty. A failed lookup yields a nil name rather than failing the response:
// the amount is the point of the row, and a missing name renders as
// unattributed.
type payerNamer struct {
	srv   *Server
	cache map[string]*string
}

func (s *Server) newPayerNamer() *payerNamer {
	return &payerNamer{srv: s, cache: map[string]*string{}}
}

func (p *payerNamer) name(ctx context.Context, userID *string) *string {
	if userID == nil {
		return nil
	}
	if cached, ok := p.cache[*userID]; ok {
		return cached
	}
	var name *string
	if user, err := p.srv.Store.GetUserByID(ctx, *userID); err == nil {
		n := user.DisplayName
		name = &n
	}
	p.cache[*userID] = name
	return name
}

func (p *payerNamer) toResponse(ctx context.Context, e db.Expense) expenseResponse {
	return expenseResponse{
		ID:               e.ID,
		TripID:           e.TripID,
		Title:            e.Title,
		AmountMinor:      e.AmountMinor,
		SpentOn:          e.SpentOn,
		PayerUserID:      e.PayerUserID,
		PayerDisplayName: p.name(ctx, e.PayerUserID),
		CreatedAt:        e.CreatedAt.UTC().Format(time.RFC3339),
	}
}

// expenseRequest is the body of both create and update.
//
// AmountMinor is a pointer so an absent field is distinguishable from an
// explicit zero. Both are refused, but with different wording, and "amount is
// required" versus "amount must be greater than zero" are genuinely different
// mistakes to have made.
//
// PayerUserID absent or null means unattributed, in both verbs, and the server
// never fills it in. The plan for this stage said absent should default to the
// caller; that was dropped once the tests made the cost visible.
//
// Two reasons. Go cannot tell an absent *string from an explicit null, so
// "absent means me" also silently means "null means me" -- there would be no
// way to record an expense somebody outside the trip paid for. And on update
// the default is outright wrong: a PATCH that replaces every field it names
// would reassign somebody else's expense to whoever edited its title. A rule
// that has to differ per verb to stay safe is the wrong rule.
//
// So defaulting to yourself lives in the client, where the form already has a
// default and the user can see it. A caller that forgets the field records an
// unattributed expense -- visible and correctable, rather than money quietly
// attributed to the wrong person.
type expenseRequest struct {
	Title       string  `json:"title"`
	AmountMinor *int64  `json:"amount_minor"`
	SpentOn     string  `json:"spent_on"`
	PayerUserID *string `json:"payer_user_id"`
}

func (req expenseRequest) validate() error {
	if strings.TrimSpace(req.Title) == "" {
		return errors.New("title is required")
	}
	if req.AmountMinor == nil {
		return errors.New("amount is required")
	}
	// Mirrors the CHECK constraint in migration 0011. Checked here as well so
	// the answer is a 400 naming the field rather than a 500 from a constraint
	// violation the client cannot read.
	if *req.AmountMinor <= 0 {
		return errors.New("amount must be greater than zero")
	}
	if strings.TrimSpace(req.SpentOn) == "" {
		return errors.New("date is required")
	}
	if _, err := time.Parse("2006-01-02", req.SpentOn); err != nil {
		return errors.New("dates must be in YYYY-MM-DD format")
	}
	return nil
}

func (s *Server) handleListExpenses(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleViewer)
	if !ok {
		return
	}
	expenses, err := s.Store.ListExpensesByTrip(r.Context(), trip.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list expenses")
		return
	}
	total, err := s.Store.SumExpensesByTrip(r.Context(), trip.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list expenses")
		return
	}

	namer := s.newPayerNamer()
	rows := make([]expenseResponse, len(expenses))
	for i, e := range expenses {
		rows[i] = namer.toResponse(r.Context(), e)
	}
	writeJSON(w, http.StatusOK, expenseListResponse{
		Currency:   trip.Currency,
		TotalMinor: total,
		Expenses:   rows,
	})
}

func (s *Server) handleCreateExpense(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req expenseRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if !s.requireTripMember(w, r, trip, req.PayerUserID) {
		return
	}

	expense, err := s.Store.CreateExpense(r.Context(), db.CreateExpenseParams{
		ID:          uuid.NewString(),
		TripID:      trip.ID,
		Title:       strings.TrimSpace(req.Title),
		AmountMinor: *req.AmountMinor,
		SpentOn:     req.SpentOn,
		PayerUserID: req.PayerUserID,
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create expense")
		return
	}
	writeJSON(w, http.StatusCreated, s.newPayerNamer().toResponse(r.Context(), expense))
}

func (s *Server) handleUpdateExpense(w http.ResponseWriter, r *http.Request) {
	expense, _, ok := s.loadExpense(w, r, db.RoleEditor)
	if !ok {
		return
	}
	trip, err := s.Store.GetTripByID(r.Context(), expense.TripID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load trip")
		return
	}

	var req expenseRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.requireTripMember(w, r, trip, req.PayerUserID) {
		return
	}

	updated, err := s.Store.UpdateExpense(r.Context(), db.UpdateExpenseParams{
		ID:          expense.ID,
		TripID:      expense.TripID,
		Title:       strings.TrimSpace(req.Title),
		AmountMinor: *req.AmountMinor,
		SpentOn:     req.SpentOn,
		PayerUserID: req.PayerUserID,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "expense not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not update expense")
		}
		return
	}
	writeJSON(w, http.StatusOK, s.newPayerNamer().toResponse(r.Context(), updated))
}

func (s *Server) handleDeleteExpense(w http.ResponseWriter, r *http.Request) {
	expense, _, ok := s.loadExpense(w, r, db.RoleEditor)
	if !ok {
		return
	}
	deleted, err := s.Store.DeleteExpense(r.Context(), expense.ID, expense.TripID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete expense")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "expense not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
