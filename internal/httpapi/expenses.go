package httpapi

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"caravel/internal/auth"
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
	// ShareUserIDs is who this expense was for, always the *effective* set:
	// an expense with no stored shares comes back listing everyone on the trip
	// rather than an empty array. So the client never implements the
	// empty-means-everyone rule, and cannot disagree with the server about it.
	ShareUserIDs []string `json:"share_user_ids"`
	// ShareMinor is what one of those people owes for this expense, from
	// splitAmount -- the reading user's own share when they are among them, and
	// absent when they are not. The whole map is not sent: the client shows
	// "your share", and the balances endpoint is where the full picture lives.
	ShareMinor *int64 `json:"share_minor"`
	// ItemID is the location this expense was for, or null -- which most
	// expenses are. It exists to answer "what was this, exactly" a month later:
	// the location holds the picture, the address and the notes, and the client
	// renders this as a link to it.
	ItemID *string `json:"item_id"`
	// ItemTitle saves the client resolving that id, for the same reason
	// PayerDisplayName does: the expenses tab does not load the trip locations
	// and should not have to. Null whenever ItemID is.
	ItemTitle *string `json:"item_title"`
	CreatedAt string  `json:"created_at"`
}

// payerTotalResponse is what one person has paid across the whole trip.
//
// Server-side rather than summed in the client, for the same reason
// total_minor is: an aggregate should have one implementation. This one is
// plain integer addition today, but Milestone 6's balances are the same
// grouping with a division on top, and having two places that decide what
// somebody paid is how the ledger and the balance come to disagree.
//
// UserID and DisplayName are both nullable, and both nil on the row standing
// for expenses with no payer -- which is one row, not one per unattributed
// expense.
type payerTotalResponse struct {
	UserID      *string `json:"user_id"`
	DisplayName *string `json:"display_name"`
	PaidMinor   int64   `json:"paid_minor"`
}

// expenseListResponse carries the currency, the total and the per-person totals
// alongside the rows. All three are on the envelope rather than left to the
// client: the total is summed by the database, so it stays right even for a
// client showing part of the list, and a currency sent explicitly is one the
// client never has to infer.
type expenseListResponse struct {
	Currency   string            `json:"currency"`
	TotalMinor int64             `json:"total_minor"`
	Expenses   []expenseResponse `json:"expenses"`
	// Payers holds one row per person who has actually paid for something,
	// plus at most one for the unattributed expenses. Somebody on the trip who
	// has paid nothing is deliberately absent: this answers "who paid", and a
	// list of zeroes answers nothing. Balances below is where paying nothing
	// becomes interesting, and that one does list everybody.
	Payers []payerTotalResponse `json:"payers"`
	// Balances is who owes whom, computed server-side so the rounding rule in
	// splitAmount has exactly one implementation. Sent on every listing rather
	// than behind its own endpoint: it is derived from the expenses, the shares
	// and the member list, all of which this handler has already loaded, so a
	// separate route would re-read the same three things.
	Balances balancesResponse `json:"balances"`
}

// expenseNamer resolves the ids in an expense row -- the payer, and the
// location it names -- to display names for one response, caching so a list of
// twenty expenses paid by two people costs two lookups rather than twenty. A
// failed lookup yields a nil name rather than failing the response: the amount
// is the point of the row, and a missing name renders as unattributed.
//
// Renamed from payerNamer in Stage 22: it resolves location titles as well now,
// which is the same job (an id in a row, a name in the response, cached per
// request) and did not deserve a second cache beside it.
type expenseNamer struct {
	srv    *Server
	cache  map[string]*string
	titles map[string]*string
}

func (s *Server) newExpenseNamer() *expenseNamer {
	return &expenseNamer{srv: s, cache: map[string]*string{}, titles: map[string]*string{}}
}

func (p *expenseNamer) name(ctx context.Context, userID *string) *string {
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

// itemTitle resolves the location an expense names. Same shape as name above,
// including the failure mode: a location that has been deleted since (the
// column is ON DELETE SET NULL, so this should not happen) yields a nil title
// rather than failing the row, because the amount is the point of the row.
func (p *expenseNamer) itemTitle(ctx context.Context, itemID *string) *string {
	if itemID == nil {
		return nil
	}
	if cached, ok := p.titles[*itemID]; ok {
		return cached
	}
	var title *string
	if item, err := p.srv.Store.GetItemByID(ctx, *itemID); err == nil {
		t := item.Title
		title = &t
	}
	p.titles[*itemID] = title
	return title
}

// toResponse builds one row. shareIDs is the effective share set, already
// resolved by the caller, and readerID is whose share to report.
func (p *expenseNamer) toResponse(ctx context.Context, e db.Expense, shareIDs []string, readerID string) expenseResponse {
	resp := expenseResponse{
		ID:               e.ID,
		TripID:           e.TripID,
		Title:            e.Title,
		AmountMinor:      e.AmountMinor,
		SpentOn:          e.SpentOn,
		PayerUserID:      e.PayerUserID,
		PayerDisplayName: p.name(ctx, e.PayerUserID),
		ShareUserIDs:     shareIDs,
		ItemID:           e.ItemID,
		ItemTitle:        p.itemTitle(ctx, e.ItemID),
		CreatedAt:        e.CreatedAt.UTC().Format(time.RFC3339),
	}
	if share, ok := splitAmount(e.AmountMinor, shareIDs)[readerID]; ok {
		resp.ShareMinor = &share
	}
	return resp
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
	// ShareUserIDs is who the expense was for. Absent or empty means everyone
	// on the trip, and stores no rows at all -- so a member added later shares
	// in it too. Naming a subset pins it to those people. Every id must hold a
	// role on the trip; duplicates are ignored rather than refused, since a
	// repeated name is redundant rather than wrong.
	ShareUserIDs []string `json:"share_user_ids"`
	// ItemID optionally names the location this expense was for. Absent or null
	// means none, on both verbs -- an expense is edited as a whole here, the way
	// the four fields above already are, so a PATCH that omits it clears it.
	// The id must name a location on this trip, which is checked rather than
	// trusted: items carry their own trip_id, so the column cannot express it.
	ItemID *string `json:"item_id"`
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

// payerTotals groups expenses by who paid, in a stable order: most paid first,
// then by name, with the unattributed row last wherever its amount would put
// it. Deterministic ordering matters more than it looks -- without it the
// summary reshuffles on every reload for two people who paid the same amount.
func payerTotals(ctx context.Context, expenses []db.Expense, names *expenseNamer) []payerTotalResponse {
	// Keyed by id, with the empty string standing for "nobody". Safe as a
	// sentinel because a user id is a UUID and never empty.
	totals := map[string]int64{}
	order := []string{}
	for _, e := range expenses {
		key := ""
		if e.PayerUserID != nil {
			key = *e.PayerUserID
		}
		if _, seen := totals[key]; !seen {
			order = append(order, key)
		}
		totals[key] += e.AmountMinor
	}

	rows := make([]payerTotalResponse, 0, len(order))
	for _, key := range order {
		row := payerTotalResponse{PaidMinor: totals[key]}
		if key != "" {
			id := key
			row.UserID = &id
			row.DisplayName = names.name(ctx, &id)
		}
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].PaidMinor != rows[j].PaidMinor {
			return rows[i].PaidMinor > rows[j].PaidMinor
		}
		// A nil name sorts last, so the unattributed row does not jump above a
		// named person who paid the same amount.
		if (rows[i].DisplayName == nil) != (rows[j].DisplayName == nil) {
			return rows[j].DisplayName == nil
		}
		if rows[i].DisplayName == nil {
			return false
		}
		return *rows[i].DisplayName < *rows[j].DisplayName
	})
	return rows
}

// resolveShares checks a requested share set against the trip and returns what
// to store: nil for "everyone", which is stored as no rows at all.
//
// Writes the error response itself when an id names somebody who is not on the
// trip. 400 rather than 403, for the same reason requireTripMember does: the
// caller is authorized, the request is what is wrong.
func (s *Server) resolveShares(w http.ResponseWriter, r *http.Request, trip db.Trip, requested []string) ([]string, bool) {
	requested = dedupeIDs(requested)
	if len(requested) == 0 {
		return nil, true
	}

	participants, err := s.tripParticipantIDs(r.Context(), trip)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not check trip membership")
		return nil, false
	}
	for _, id := range requested {
		if !slices.Contains(participants, id) {
			writeError(w, http.StatusBadRequest, "an expense cannot be shared with somebody who is not on this trip")
			return nil, false
		}
	}

	// A set naming everybody is stored as no rows, so it keeps behaving as
	// "everyone" when the trip gains a member. Otherwise saving an unrelated
	// edit would silently pin an expense that was never pinned -- and the
	// client has no way to tell it had happened.
	if len(requested) == len(participants) {
		return nil, true
	}
	return requested, true
}

// writeShares replaces an expense's share set. Runs inside the caller's
// transaction: a half-written set is a wrong split, not a missing one.
func writeShares(ctx context.Context, store db.Store, expenseID string, shareIDs []string) error {
	if err := store.DeleteExpenseSharesByExpense(ctx, expenseID); err != nil {
		return err
	}
	for _, id := range shareIDs {
		if err := store.CreateExpenseShare(ctx, expenseID, id); err != nil {
			return err
		}
	}
	return nil
}

// effectiveShares is the empty-means-everyone rule, in the one place that
// implements it. stored is what the table holds for an expense.
func effectiveShares(stored, participants []string) []string {
	if len(stored) > 0 {
		return stored
	}
	return participants
}

// requireTripItem checks that an item id in a request body names a location on
// this trip, the way requireTripMember checks the payer. Returns false having
// already answered, so callers read as a guard.
//
// A 400 rather than a 404: the caller sent a field this trip cannot accept,
// which is a bad request about a resource they can see, not a missing one. Same
// answer requireSameTrip gives for a media asset from another trip.
func (s *Server) requireTripItem(w http.ResponseWriter, r *http.Request, trip db.Trip, itemID *string) bool {
	if itemID == nil {
		return true
	}
	item, err := s.Store.GetItemByID(r.Context(), *itemID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusBadRequest, "location not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not load the location")
		}
		return false
	}
	return s.requireSameTrip(w, item.TripID, trip.ID, "location belongs to another trip")
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
	// Every share on the trip in one query, then grouped here: asking per
	// expense would be a query per row.
	shares, err := s.Store.ListExpenseSharesByTrip(r.Context(), trip.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list expenses")
		return
	}
	participants, err := s.tripParticipantIDs(r.Context(), trip)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list expenses")
		return
	}
	byExpense := map[string][]string{}
	for _, share := range shares {
		byExpense[share.ExpenseID] = append(byExpense[share.ExpenseID], share.UserID)
	}

	me, _ := auth.UserFromContext(r.Context())
	namer := s.newExpenseNamer()
	rows := make([]expenseResponse, len(expenses))
	for i, e := range expenses {
		rows[i] = namer.toResponse(r.Context(), e, effectiveShares(byExpense[e.ID], participants), me.ID)
	}
	// The effective share set per expense, which is what the balance arithmetic
	// needs and what the rows above were already built from.
	effective := map[string][]string{}
	for _, e := range expenses {
		effective[e.ID] = effectiveShares(byExpense[e.ID], participants)
	}

	writeJSON(w, http.StatusOK, expenseListResponse{
		Currency:   trip.Currency,
		TotalMinor: total,
		Expenses:   rows,
		// The same namer throughout, so the per-person rows and the balances
		// cost no extra lookups: every payer was resolved building the rows
		// above, and the balance names come from the same cache.
		Payers: payerTotals(r.Context(), expenses, namer),
		Balances: computeBalances(expenses, effective, participants, func(id string) *string {
			return namer.name(r.Context(), &id)
		}),
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
	if !s.requireTripItem(w, r, trip, req.ItemID) {
		return
	}
	shareIDs, ok := s.resolveShares(w, r, trip, req.ShareUserIDs)
	if !ok {
		return
	}

	// One transaction for the expense and its shares. A create that half
	// succeeded would leave an expense split between the wrong people, which
	// is worse than one that failed: the total still looks right.
	var expense db.Expense
	err := s.Store.WithTx(r.Context(), func(store db.Store) error {
		created, err := store.CreateExpense(r.Context(), db.CreateExpenseParams{
			ID:          uuid.NewString(),
			TripID:      trip.ID,
			Title:       strings.TrimSpace(req.Title),
			AmountMinor: *req.AmountMinor,
			SpentOn:     req.SpentOn,
			PayerUserID: req.PayerUserID,
			ItemID:      req.ItemID,
			CreatedAt:   time.Now().UTC(),
		})
		if err != nil {
			return err
		}
		expense = created
		return writeShares(r.Context(), store, created.ID, shareIDs)
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create expense")
		return
	}
	s.writeExpense(w, r, trip, expense, http.StatusCreated)
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
	if !s.requireTripItem(w, r, trip, req.ItemID) {
		return
	}
	shareIDs, ok := s.resolveShares(w, r, trip, req.ShareUserIDs)
	if !ok {
		return
	}

	var updated db.Expense
	err = s.Store.WithTx(r.Context(), func(store db.Store) error {
		changed, err := store.UpdateExpense(r.Context(), db.UpdateExpenseParams{
			ID:          expense.ID,
			TripID:      expense.TripID,
			Title:       strings.TrimSpace(req.Title),
			AmountMinor: *req.AmountMinor,
			SpentOn:     req.SpentOn,
			PayerUserID: req.PayerUserID,
			ItemID:      req.ItemID,
		})
		if err != nil {
			return err
		}
		updated = changed
		// Replaced wholesale rather than patched: the request states who the
		// expense is for, so anybody it does not name is no longer among them.
		return writeShares(r.Context(), store, expense.ID, shareIDs)
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "expense not found")
		} else {
			writeError(w, http.StatusInternalServerError, "could not update expense")
		}
		return
	}
	s.writeExpense(w, r, trip, updated, http.StatusOK)
}

// writeExpense is the shared tail of the handlers returning a single expense:
// the response needs the effective share set, and resolving that in two places
// invited one of them to drift. Same reasoning as writeChecklist.
func (s *Server) writeExpense(w http.ResponseWriter, r *http.Request, trip db.Trip, e db.Expense, status int) {
	stored, err := s.Store.ListExpenseShareUsers(r.Context(), e.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load expense shares")
		return
	}
	participants, err := s.tripParticipantIDs(r.Context(), trip)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load trip members")
		return
	}
	me, _ := auth.UserFromContext(r.Context())
	writeJSON(w, status, s.newExpenseNamer().toResponse(r.Context(), e, effectiveShares(stored, participants), me.ID))
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
