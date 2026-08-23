package httpapi

import (
	"sort"

	"caravel/internal/db"
)

// Balances: who owes whom. Stage 17 Milestone 6.
//
// Computed here and nowhere else. A balance recomputed in the client would be a
// second implementation of the rounding rule in splitAmount, and the two would
// eventually disagree -- at which point the app shows two different answers to
// "what do I owe" depending on which screen you are on.
//
// The arithmetic is one line per person: what they paid, minus what their
// shares add up to. Everything difficult is in deciding what to *count*.

// balancePerson is one person's standing on the trip. Net is Paid - Owed, so
// positive means the trip owes them.
type balancePerson struct {
	UserID      string  `json:"user_id"`
	DisplayName *string `json:"display_name"`
	PaidMinor   int64   `json:"paid_minor"`
	OwedMinor   int64   `json:"owed_minor"`
	NetMinor    int64   `json:"net_minor"`
}

// balanceTransfer is one suggested payment that reduces two people's nets
// toward zero.
type balanceTransfer struct {
	FromUserID      string  `json:"from_user_id"`
	FromDisplayName *string `json:"from_display_name"`
	ToUserID        string  `json:"to_user_id"`
	ToDisplayName   *string `json:"to_display_name"`
	AmountMinor     int64   `json:"amount_minor"`
}

type balancesResponse struct {
	People    []balancePerson   `json:"people"`
	Transfers []balanceTransfer `json:"transfers"`
	// UnattributedMinor is the money that could not be attributed to anyone,
	// because its expenses record no payer. Those expenses are left out of the
	// balance entirely -- see computeBalances -- and this is what says so, so
	// the client can report it rather than presenting a total that quietly
	// excludes something.
	UnattributedMinor int64 `json:"unattributed_minor"`
}

// computeBalances works out each participant's net and a set of transfers that
// settles it.
//
// sharesByExpense holds the *effective* share set per expense, so the
// empty-means-everyone rule has already been applied by the caller.
//
// Expenses with no payer are excluded from both sides, not just from the paid
// side. That is the only choice that keeps the books straight: their shares
// would put people in debt to nobody, so the nets would sum to minus the
// unattributed total instead of to zero, and any transfer suggestion built on
// that is arithmetic about money that does not exist. Their total is reported
// separately instead. Splitting them silently across the trip would be a
// confidently wrong number, which is worse than an incomplete one.
func computeBalances(expenses []db.Expense, sharesByExpense map[string][]string, participants []string, names func(string) *string) balancesResponse {
	paid := map[string]int64{}
	owed := map[string]int64{}
	var unattributed int64

	for _, e := range expenses {
		if e.PayerUserID == nil {
			unattributed += e.AmountMinor
			continue
		}
		paid[*e.PayerUserID] += e.AmountMinor
		for id, share := range splitAmount(e.AmountMinor, sharesByExpense[e.ID]) {
			owed[id] += share
		}
	}

	// Everyone on the trip appears, even at zero: a balance section that
	// omitted somebody would read as "they are settled up" when it might mean
	// "we forgot them". Anybody who paid or owes but is no longer on the trip
	// appears too -- they left, the money did not.
	ids := map[string]bool{}
	for _, id := range participants {
		ids[id] = true
	}
	for id := range paid {
		ids[id] = true
	}
	for id := range owed {
		ids[id] = true
	}

	people := make([]balancePerson, 0, len(ids))
	for id := range ids {
		people = append(people, balancePerson{
			UserID:      id,
			DisplayName: names(id),
			PaidMinor:   paid[id],
			OwedMinor:   owed[id],
			NetMinor:    paid[id] - owed[id],
		})
	}
	// Creditors first, then debtors, ties broken by id. Deterministic because
	// the transfers below are derived from this order: without it the same
	// ledger would suggest different payments on different reads.
	sort.Slice(people, func(i, j int) bool {
		if people[i].NetMinor != people[j].NetMinor {
			return people[i].NetMinor > people[j].NetMinor
		}
		return people[i].UserID < people[j].UserID
	})

	return balancesResponse{
		People:            people,
		Transfers:         suggestTransfers(people),
		UnattributedMinor: unattributed,
	}
}

// suggestTransfers turns a set of nets into payments that zero them out.
//
// Greedy largest-debtor against largest-creditor. This is not guaranteed to be
// the theoretical minimum number of payments -- that problem is NP-hard -- but
// it never exceeds one fewer than the number of people involved, which is the
// bound that matters when the group is four friends rather than a bank.
//
// people must already be sorted by net descending, which computeBalances
// guarantees; the result is then deterministic for a given ledger.
func suggestTransfers(people []balancePerson) []balanceTransfer {
	type account struct {
		id     string
		name   *string
		amount int64 // positive: owed money. negative: owes money.
	}

	var creditors, debtors []account
	for _, p := range people {
		switch {
		case p.NetMinor > 0:
			creditors = append(creditors, account{p.UserID, p.DisplayName, p.NetMinor})
		case p.NetMinor < 0:
			debtors = append(debtors, account{p.UserID, p.DisplayName, -p.NetMinor})
		}
	}
	// people is net-descending, so creditors already run largest-first and
	// debtors smallest-debt-first. Reverse the latter to take the largest debt
	// first, which is what keeps the number of transfers down.
	for i, j := 0, len(debtors)-1; i < j; i, j = i+1, j-1 {
		debtors[i], debtors[j] = debtors[j], debtors[i]
	}

	transfers := []balanceTransfer{}
	for d, c := 0, 0; d < len(debtors) && c < len(creditors); {
		amount := min(debtors[d].amount, creditors[c].amount)
		if amount > 0 {
			transfers = append(transfers, balanceTransfer{
				FromUserID:      debtors[d].id,
				FromDisplayName: debtors[d].name,
				ToUserID:        creditors[c].id,
				ToDisplayName:   creditors[c].name,
				AmountMinor:     amount,
			})
		}
		debtors[d].amount -= amount
		creditors[c].amount -= amount
		if debtors[d].amount == 0 {
			d++
		}
		if creditors[c].amount == 0 {
			c++
		}
	}
	return transfers
}
