// What the current user may do on a trip.
//
// The API puts the *reading* user's role in every trip payload (`trip.role`,
// one of "owner" / "editor" / "viewer"), so the client never has to discover a
// permission from a 403. These helpers are the only place that string is
// interpreted: a tab or component asks `canEdit(trip)` rather than comparing
// role names itself, so adding a role later changes one file.
//
// Server-side enforcement lives in internal/httpapi/authz.go and is the real
// boundary. Everything here is about not *offering* an action that would be
// refused — hiding a button is a courtesy, not a security control.

const RANK = { viewer: 1, editor: 2, owner: 3 };

// atLeast is deliberately conservative about an unknown or missing role: a
// payload without one ranks 0 and can do nothing. A trip fetched before this
// field existed, or a role a future server adds, must not read as permissive.
function atLeast(trip, min) {
  const rank = RANK[trip?.role] || 0;
  return rank > 0 && rank >= RANK[min];
}

/** Can the user change this trip's content — locations, itinerary, files, checklists? */
export function canEdit(trip) {
  return atLeast(trip, "editor");
}

/** Can the user add or remove members, or delete the trip? Owner only. */
export function canManageMembers(trip) {
  return atLeast(trip, "owner");
}

/** Is the user read-only here? Distinct from !canEdit, which is also true for a trip with no role at all. */
export function isViewer(trip) {
  return trip?.role === "viewer";
}

/** Does the user own this trip? */
export function isOwner(trip) {
  return trip?.role === "owner";
}
