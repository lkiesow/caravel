package assist

// The values crossing this package's boundary. They are deliberately plain
// data: no db types, no HTTP types, nothing that would make internal/assist
// depend on the storage layer or the transport.

// Mode selects what the agent is being asked to do.
type Mode string

const (
	// ModeEnrich fills in what is missing on a location that already exists,
	// using its current metadata as the starting point.
	ModeEnrich Mode = "enrich"
	// ModePrompt builds a location from a free-text description, with no
	// existing metadata to work from.
	ModePrompt Mode = "prompt"
)

// Valid reports whether m is a mode the agent understands. Used to reject a
// request body rather than defaulting, because the two modes are genuinely
// different asks and guessing wrong wastes a paid run.
func (m Mode) Valid() bool { return m == ModeEnrich || m == ModePrompt }

// Request is one enrichment run.
type Request struct {
	Mode Mode
	// Prompt is the user's free-text description. Required for ModePrompt,
	// optional for ModeEnrich, where it narrows what to look for
	// ("find the check-in times").
	Prompt string

	// Current is the location's existing metadata. Enriching means knowing
	// what is already there -- both so the agent does not propose what is
	// already written, and so the caller can show a before/after.
	Current Location

	// TypeVocabulary is the distinct Type values already used on this trip.
	// Type is free text, so left alone a model will produce "hotel", "Hotel"
	// and "hostel" for the same three stays and fragment the filter. Sending
	// what is in use and asking it to reuse one where it fits is cheaper than
	// normalising afterwards, which would need a synonym table nobody wants
	// to maintain.
	TypeVocabulary []string

	// Trip is the surrounding trip's title and dates, or the zero value when
	// the user has unticked the box that sends them. Dates in particular help
	// a lot -- "is it open in November" is unanswerable without them -- but
	// they are also the most personal thing in the payload, so it is a choice
	// rather than a default.
	Trip TripContext

	// Locale is the user's UI language as a BCP-47 tag. Notes come back in it;
	// a German user should not get English prose in their own trip.
	Locale string
}

// Location is the metadata the agent reads and proposes. The same struct in
// both directions, so a proposal is directly comparable to what is there.
type Location struct {
	Title    string
	Category string // "site" | "stay" | "transport" -- validated, never trusted
	Type     string // free text, steered by Request.TypeVocabulary
	Notes    string // markdown
	Address  string
	Links    []Link
}

// TripContext is the optional surrounding-trip information. Zero value means
// the user chose not to send it.
type TripContext struct {
	Title string
	Start string // "YYYY-MM-DD", empty if the trip has no dates
	End   string
}

// Sent reports whether any trip context is actually present, so callers can
// tell "no trip context" from "a trip with an empty title".
func (t TripContext) Sent() bool { return t.Title != "" || t.Start != "" || t.End != "" }

// Link is a URL with an optional label, matching db.ItemLink minus the ids.
type Link struct {
	URL   string
	Label string
}

// Proposal is what a run produces. Every field is a suggestion; the caller
// decides what, if anything, is applied.
type Proposal struct {
	// Fields is one entry per proposed scalar change, carrying the current
	// value alongside the proposed one. Built as pairs rather than as a
	// Location so the UI can show a before/after without re-deriving which
	// fields changed, and so a field the agent chose not to touch is absent
	// rather than present-and-identical.
	Fields []Field

	// Links the agent proposes adding. Each has survived a liveness check
	// (see agent.go): hallucinated URLs are the classic failure of this kind
	// of feature, and a dead link is worse than no link because it looks
	// authoritative until clicked.
	Links []Link

	// Coordinates resolved from the proposed address by internal/geocode --
	// never values the model produced. Nil when the address could not be
	// resolved, or when no geocoder is configured.
	Lat *float64
	Lng *float64

	// Sources the agent actually consulted, shown with the proposal so a
	// person can judge it. Stored nowhere: once the proposal is accepted it is
	// just the user's data, and a provenance trail nobody asked for is a
	// retention decision made by accident.
	Sources []Source
}

// Field is one proposed scalar change.
type Field struct {
	// Name is the location field this applies to: "title", "category",
	// "type", "notes" or "address".
	Name string
	// Current is what the form holds now, empty if nothing.
	Current string
	// Proposed is what the agent suggests. Never empty -- an agent that wants
	// to clear a field is not a thing this feature offers, because "the AI
	// deleted my notes" is exactly the outcome the per-field review exists to
	// prevent.
	Proposed string
}

// Overwrites reports whether accepting this field would replace existing
// content, which is the case the UI must show as a before/after rather than as
// a plain suggestion.
func (f Field) Overwrites() bool { return f.Current != "" }

// Source is a page the agent read.
type Source struct {
	Title string
	URL   string
}

// Event is a progress report from a run in flight.
type Event struct {
	// Key is an i18n key ("assist.progress.searching"), not a sentence. The
	// server does not write UI copy: it does not know the user's language,
	// and a translated string arriving over the wire cannot be re-rendered
	// when the user switches locale mid-run.
	Key string
	// Params fills placeholders in the translated string -- the search term,
	// the host being fetched. Values are data, so the client must escape them
	// on render: a page title from a search result is attacker-influenced.
	Params map[string]string
}
