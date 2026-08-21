// The signed-in user, as /auth/me last reported them.
//
// app.js fetches that once at boot and after a login, and pages that need
// something out of it read here instead of fetching again. Today the only
// caller is the location editor asking whether address search is available -
// a *server* capability that rides along on /auth/me because it is the one
// call the app already makes (see userResponse in internal/httpapi/auth.go).
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
