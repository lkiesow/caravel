package assist

// The names of the tools the model may call.
//
// Declared here in Milestone 2 because they are a contract between three
// things that land in different milestones: the stub provider's script (which
// calls them), the tool dispatcher (Milestone 3, which implements them) and
// the agent loop (Milestone 4, which routes between the two). Keeping the
// names in one place means the stub cannot drift into scripting a call that
// no longer exists -- a failure that would otherwise show up as an agent
// mysteriously giving up.
const (
	// toolWebSearch searches the web. Absent when no search provider is
	// configured, in which case the agent still runs -- worse, but working.
	toolWebSearch = "web_search"
	// toolFetchPage retrieves one page as text. The tool with a real attack
	// surface, guarded in fetch.go.
	toolFetchPage = "fetch_page"
	// toolGeocode resolves a place name or address to coordinates through
	// OpenStreetMap. Available to the model as a lookup, but note that the
	// coordinates that reach the proposal are resolved by the agent itself,
	// not taken from whatever the model does with this.
	toolGeocode = "geocode"
)
