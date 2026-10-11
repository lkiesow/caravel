// Where Caravel itself lives, for the About dialog and the login footer. One
// module so that a moved repository is one edit, not a hunt.
//
// The server builds its own links to releases and commits from the same repo
// (internal/buildinfo.RepoURL) and hands them over on /auth/me; these are the
// ones that do not depend on which build is running.
export const WEBSITE_URL = "https://explore-caravel.org/";
export const REPO_URL = "https://github.com/lkiesow/caravel";
export const ISSUES_URL = `${REPO_URL}/issues/new`;
