// The signed-in user, as /auth/me last reported them.
//
// app.js fetches that once at boot and after a login, and pages that need
// something out of it read here instead of fetching again.
//
// Deliberately a plain module variable rather than anything reactive: the
// values in it do not change while a session lasts, and a page that renders
// after boot has always already got them.
let currentUser = null;

export function setCurrentUser(user) {
  currentUser = user;
}

export function getCurrentUser() {
  return currentUser;
}

// Whether the *server* is configured for something -- address search, the
// assistant, image search. These are not permissions and say nothing about the
// user; they ride along on /auth/me because it is the one call the app already
// makes at boot (see capabilitiesResponse in internal/httpapi/auth.go).
//
// A helper rather than three call sites reaching through two levels of optional
// chaining: the shape of that payload is then known in exactly one place, which
// is the lesson of the flat-flags version this replaced. False when the user is
// not loaded yet, which is the safe answer -- a control that needs a capability
// should not render before the app knows whether it exists.
export function hasCapability(name) {
  return Boolean(currentUser?.capabilities?.[name]);
}
