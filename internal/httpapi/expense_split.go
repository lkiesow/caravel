package httpapi

import "sort"

// Splitting an expense between the people it was for. Stage 17 Milestone 5.
//
// This is the whole reason amounts are integers in a currency's minor unit: an
// equal split of an odd amount does not exist, so somebody has to be a unit
// out, and the only question is whether the code says who or lets floating
// point decide. 1000 across three people is 334 + 333 + 333, never 333.33
// three times -- which sums to 999.99 and leaves a cent belonging to nobody.
//
// The extra units go to the lowest user ids. Any rule would do as long as it is
// *a* rule: what matters is that the same ledger always produces the same
// split, so a balance does not change when a list is read twice.
func splitAmount(amountMinor int64, userIDs []string) map[string]int64 {
	shares := make(map[string]int64, len(userIDs))
	if len(userIDs) == 0 {
		// Nobody to split between. The caller has already resolved an empty
		// share set to everyone on the trip, and a trip always has an owner,
		// so this is unreachable -- and returning an empty map rather than
		// dividing by zero is the difference between a missing row and a panic.
		return shares
	}

	sorted := make([]string, len(userIDs))
	copy(sorted, userIDs)
	sort.Strings(sorted)

	n := int64(len(sorted))
	base := amountMinor / n
	remainder := amountMinor % n
	for i, id := range sorted {
		share := base
		if int64(i) < remainder {
			share++
		}
		shares[id] = share
	}
	return shares
}

// dedupeIDs returns ids with duplicates and blanks removed, order preserved.
//
// A client naming the same person twice must not double their weight in the
// split. The table's primary key refuses the second row anyway, but that would
// surface as a failed insert rolling back the whole save, which is a strange
// answer to a request that is merely redundant.
func dedupeIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}
