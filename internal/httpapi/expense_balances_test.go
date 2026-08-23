package httpapi

import (
	"testing"
	"time"

	"caravel/internal/db"
)

// Balance arithmetic, unit level. The HTTP-level tests in expenses_test.go
// cover the wiring; these cover the rules.

func expense(id, payer string, amount int64) db.Expense {
	e := db.Expense{ID: id, AmountMinor: amount, SpentOn: "2026-08-20", CreatedAt: time.Now()}
	if payer != "" {
		p := payer
		e.PayerUserID = &p
	}
	return e
}

// Stable pointers per id, cached the way payerNamer caches in production. A
// fresh pointer per call would make struct comparison test pointer identity
// rather than the values -- which is exactly what the determinism test below
// was reporting before this was a map.
var nameCache = map[string]*string{}

func namesOf(id string) *string {
	if cached, ok := nameCache[id]; ok {
		return cached
	}
	n := "name-" + id
	nameCache[id] = &n
	return &n
}

// netOf is a lookup helper: the people list is sorted by net, so indexing it
// would make every test depend on that order.
func netOf(t *testing.T, b balancesResponse, id string) balancePerson {
	t.Helper()
	for _, p := range b.People {
		if p.UserID == id {
			return p
		}
	}
	t.Fatalf("%s is missing from the balance: %+v", id, b.People)
	return balancePerson{}
}

// The invariant that makes a balance a balance: every net sums to zero. If it
// does not, somebody is owed money that nobody owes, and the transfers below
// are arithmetic about money that does not exist.
func assertNetsSumToZero(t *testing.T, b balancesResponse) {
	t.Helper()
	var total int64
	for _, p := range b.People {
		total += p.NetMinor
	}
	if total != 0 {
		t.Errorf("nets sum to %d, want 0: %+v", total, b.People)
	}
}

// And the invariant on the advice: following every transfer must leave everyone
// at zero. A suggestion that does not settle up is worse than none.
func assertTransfersSettle(t *testing.T, b balancesResponse) {
	t.Helper()
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
			t.Errorf("after every suggested transfer, %s is still %d from settled", id, left)
		}
	}
	// No more transfers than one fewer than the number of people involved.
	involved := 0
	for _, p := range b.People {
		if p.NetMinor != 0 {
			involved++
		}
	}
	if involved > 0 && len(b.Transfers) > involved-1 {
		t.Errorf("%d transfers for %d people with a non-zero net", len(b.Transfers), involved)
	}
}

// Two people, one pays for everything: the simplest case, and the one whose
// answer everyone can check in their head.
func TestBalancesTwoPeopleSymmetric(t *testing.T) {
	participants := []string{"alice", "bob"}
	expenses := []db.Expense{expense("e1", "alice", 1000)}
	shares := map[string][]string{"e1": participants}

	b := computeBalances(expenses, shares, participants, namesOf)
	assertNetsSumToZero(t, b)
	assertTransfersSettle(t, b)

	if got := netOf(t, b, "alice"); got.PaidMinor != 1000 || got.OwedMinor != 500 || got.NetMinor != 500 {
		t.Errorf("alice: %+v", got)
	}
	if got := netOf(t, b, "bob"); got.PaidMinor != 0 || got.OwedMinor != 500 || got.NetMinor != -500 {
		t.Errorf("bob: %+v", got)
	}
	if len(b.Transfers) != 1 {
		t.Fatalf("want one transfer, got %+v", b.Transfers)
	}
	tr := b.Transfers[0]
	if tr.FromUserID != "bob" || tr.ToUserID != "alice" || tr.AmountMinor != 500 {
		t.Errorf("transfer: %+v", tr)
	}
}

// Three people and an amount that does not divide: the remainder has to land
// somewhere, and the nets still have to sum to zero afterwards.
func TestBalancesThreePeopleWithRemainder(t *testing.T) {
	participants := []string{"alice", "bob", "carol"}
	expenses := []db.Expense{expense("e1", "alice", 1000)}
	shares := map[string][]string{"e1": participants}

	b := computeBalances(expenses, shares, participants, namesOf)
	assertNetsSumToZero(t, b)
	assertTransfersSettle(t, b)

	// 1000 across three is 334/333/333, the extra unit to the lowest id.
	if got := netOf(t, b, "alice"); got.OwedMinor != 334 || got.NetMinor != 666 {
		t.Errorf("alice: %+v", got)
	}
	if got := netOf(t, b, "bob"); got.OwedMinor != 333 || got.NetMinor != -333 {
		t.Errorf("bob: %+v", got)
	}
	if got := netOf(t, b, "carol"); got.OwedMinor != 333 || got.NetMinor != -333 {
		t.Errorf("carol: %+v", got)
	}
}

// A share set that excludes the payer. They are owed the whole amount, which is
// the case that catches an implementation that assumes the payer always shares.
func TestBalancesShareSetExcludesThePayer(t *testing.T) {
	participants := []string{"alice", "bob", "carol"}
	expenses := []db.Expense{expense("e1", "alice", 900)}
	shares := map[string][]string{"e1": {"bob", "carol"}}

	b := computeBalances(expenses, shares, participants, namesOf)
	assertNetsSumToZero(t, b)
	assertTransfersSettle(t, b)

	if got := netOf(t, b, "alice"); got.PaidMinor != 900 || got.OwedMinor != 0 || got.NetMinor != 900 {
		t.Errorf("alice paid for something she is not part of: %+v", got)
	}
	for _, id := range []string{"bob", "carol"} {
		if got := netOf(t, b, id); got.OwedMinor != 450 || got.NetMinor != -450 {
			t.Errorf("%s: %+v", id, got)
		}
	}
}

// An unattributed expense is reported, never folded in. Splitting it would
// produce a confidently wrong number, and counting only its shares would put
// people in debt to nobody -- so the nets would not sum to zero.
func TestBalancesReportUnattributedRatherThanAbsorbingIt(t *testing.T) {
	participants := []string{"alice", "bob"}
	expenses := []db.Expense{
		expense("e1", "alice", 1000),
		expense("e2", "", 300), // nobody recorded as paying
	}
	shares := map[string][]string{"e1": participants, "e2": participants}

	b := computeBalances(expenses, shares, participants, namesOf)
	assertNetsSumToZero(t, b)
	assertTransfersSettle(t, b)

	if b.UnattributedMinor != 300 {
		t.Errorf("unattributed_minor: got %d, want 300", b.UnattributedMinor)
	}
	// The unattributed 300 touches nobody's balance: alice is owed 500 for the
	// expense she actually paid, and no more.
	if got := netOf(t, b, "alice"); got.NetMinor != 500 || got.OwedMinor != 500 {
		t.Errorf("alice: %+v -- the unattributed expense must not reach her balance", got)
	}
	if got := netOf(t, b, "bob"); got.NetMinor != -500 {
		t.Errorf("bob: %+v", got)
	}
}

// Everybody appears, including somebody who has neither paid nor owes. A
// balance section that omitted them would read as "settled up" when it might
// mean "we forgot them".
func TestBalancesIncludeUninvolvedParticipants(t *testing.T) {
	participants := []string{"alice", "bob", "dave"}
	expenses := []db.Expense{expense("e1", "alice", 1000)}
	shares := map[string][]string{"e1": {"alice", "bob"}}

	b := computeBalances(expenses, shares, participants, namesOf)
	if len(b.People) != 3 {
		t.Fatalf("want all three participants, got %+v", b.People)
	}
	if got := netOf(t, b, "dave"); got.NetMinor != 0 || got.PaidMinor != 0 || got.OwedMinor != 0 {
		t.Errorf("dave: %+v", got)
	}
	// And nobody suggests paying him anything.
	for _, tr := range b.Transfers {
		if tr.FromUserID == "dave" || tr.ToUserID == "dave" {
			t.Errorf("dave has nothing to settle but appears in a transfer: %+v", tr)
		}
	}
}

// Somebody who paid and then left the trip is not a participant any more, but
// the money did not leave with them.
func TestBalancesIncludeSomebodyWhoLeft(t *testing.T) {
	participants := []string{"alice", "bob"}
	expenses := []db.Expense{expense("e1", "gone", 1000)}
	shares := map[string][]string{"e1": participants}

	b := computeBalances(expenses, shares, participants, namesOf)
	assertNetsSumToZero(t, b)
	assertTransfersSettle(t, b)

	if got := netOf(t, b, "gone"); got.PaidMinor != 1000 || got.OwedMinor != 0 || got.NetMinor != 1000 {
		t.Errorf("the person who left is still owed: %+v", got)
	}
}

// A messier ledger, where the greedy matching actually has choices to make.
func TestBalancesMultiWayLedgerSettles(t *testing.T) {
	participants := []string{"alice", "bob", "carol", "dave"}
	expenses := []db.Expense{
		expense("e1", "alice", 4000),
		expense("e2", "bob", 1500),
		expense("e3", "carol", 777),
		expense("e4", "alice", 101),
	}
	shares := map[string][]string{
		"e1": participants,
		"e2": {"bob", "carol"},
		"e3": participants,
		"e4": {"dave"},
	}

	b := computeBalances(expenses, shares, participants, namesOf)
	assertNetsSumToZero(t, b)
	assertTransfersSettle(t, b)

	// Every expense is accounted for on both sides.
	var paid, owed int64
	for _, p := range b.People {
		paid += p.PaidMinor
		owed += p.OwedMinor
	}
	if paid != 6378 || owed != 6378 {
		t.Errorf("paid %d and owed %d, want both 6378", paid, owed)
	}
}

// A settled trip suggests nothing. An empty list is the right answer, and it
// must be an empty list rather than null so the client can count it.
func TestBalancesSettledTripSuggestsNothing(t *testing.T) {
	participants := []string{"alice", "bob"}
	expenses := []db.Expense{
		expense("e1", "alice", 1000),
		expense("e2", "bob", 1000),
	}
	shares := map[string][]string{"e1": participants, "e2": participants}

	b := computeBalances(expenses, shares, participants, namesOf)
	assertNetsSumToZero(t, b)
	if len(b.Transfers) != 0 {
		t.Errorf("a settled trip should suggest no transfers, got %+v", b.Transfers)
	}
	if b.Transfers == nil {
		t.Error("transfers should be an empty list, not null")
	}
}

// The same ledger must always produce the same advice: read a trip twice and
// get two different sets of payments and neither can be trusted.
func TestBalancesAreDeterministic(t *testing.T) {
	participants := []string{"alice", "bob", "carol", "dave"}
	expenses := []db.Expense{
		expense("e1", "alice", 3000),
		expense("e2", "bob", 3000),
		expense("e3", "carol", 1),
	}
	shares := map[string][]string{"e1": participants, "e2": participants, "e3": participants}

	first := computeBalances(expenses, shares, participants, namesOf)
	for i := 0; i < 5; i++ {
		again := computeBalances(expenses, shares, participants, namesOf)
		if len(again.Transfers) != len(first.Transfers) {
			t.Fatalf("read %d suggested %d transfers, first read suggested %d", i, len(again.Transfers), len(first.Transfers))
		}
		for j := range first.Transfers {
			if again.Transfers[j] != first.Transfers[j] {
				t.Errorf("read %d transfer %d: got %+v, want %+v", i, j, again.Transfers[j], first.Transfers[j])
			}
		}
		for j := range first.People {
			if again.People[j] != first.People[j] {
				t.Errorf("read %d person %d: got %+v, want %+v", i, j, again.People[j], first.People[j])
			}
		}
	}
}
