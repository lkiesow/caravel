// assetURL(path) returns the URL to load a static file from.
//
// Here, on disk, it returns the path unchanged: that is what `make dev` serves,
// where every file is read live and nothing is cached. The production bundle
// replaces this module with a generated one (internal/webbundle) that maps each
// path to a versioned URL -- /v/<hash>/fonts/inter-400.woff2 -- which the server
// marks immutable, so a reload never has to ask whether the file changed.
//
// Use it for any static file the code names by absolute path: a fetch(), a
// dynamic import(), a <link> or <use> href in markup it builds. A url() in
// base.css needs nothing; the bundler rewrites those itself. A path the table
// does not know is passed through unchanged, so forgetting this costs speed,
// never correctness.
export function assetURL(path) {
  return path;
}
