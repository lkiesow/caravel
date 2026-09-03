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
	// ModeSuggest offers several places for a trip at once. It names the run
	// in the log and in the trace and is never read from a request body: the
	// endpoint decides it, which is why Valid below does not accept it.
	ModeSuggest Mode = "suggest"
)

// Valid reports whether m is a mode a *location* request may ask for. Used to
// reject a request body rather than defaulting, because the two modes are
// genuinely different asks and guessing wrong wastes a paid run.
//
// ModeSuggest is deliberately absent. It belongs to a different endpoint with
// a differently-shaped answer, and accepting it here would let a client ask
// /assist/location for something it cannot return.
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

	// TagVocabulary is the distinct tags already in use on this trip. Tags are
	// free text, so left alone a model will produce "hotel", "Hotel" and
	// "hostel" for the same three stays and fragment the filter. Sending what
	// is in use and asking it to reuse one where it fits is cheaper than
	// normalising afterwards, which would need a synonym table nobody wants
	// to maintain.
	//
	// This was TypeVocabulary until Stage 26 Milestone 7 retired the type
	// field into the tag set; the reasoning is unchanged, and the vocabulary
	// it now carries is larger and more useful for exactly the same purpose.
	TagVocabulary []string

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
	// Tags is a comma-separated list, not a slice. It stays a string because
	// the whole proposal pipeline -- Field{Name, Current, Proposed string},
	// the agent's diff table, the panel's allowlist and the editor's
	// applyField -- is string-shaped, and making one field polymorphic to
	// avoid one split and one join would be the more expensive change. The
	// editor splits it back into chips.
	Tags    string // free text, steered by Request.TagVocabulary
	Notes   string // markdown
	Address string
	Links   []Link
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

// SuggestRequest is one trip-level run: several places at once, rather than
// one place in depth.
//
// Deliberately not a Mode on Request. The two runs differ in what they are
// given (a location to work on, versus a trip to fill), in what they return
// (one proposal with before/after pairs, versus a list of candidates with
// nothing to diff against) and in what the caller does with the answer. One
// struct carrying both would be half-empty whichever run was asked for.
type SuggestRequest struct {
	// Prompt is what the user asked for: "things to do in Reykjavik", "a
	// morning with kids near the harbour". Required -- unlike an enrichment
	// run there is nothing else to work from.
	Prompt string

	// Existing is what the trip already has, so the run can be told not to
	// offer it again and the answer can be checked against it afterwards.
	// Deliberately not the full Location: a name and a position is everything
	// the dedup needs, and sending each place notes and links would spend the
	// budget describing what the user is not asking about.
	Existing []ExistingPlace

	// TagVocabulary, Trip and Locale mean exactly what they mean on Request,
	// and are steered by the same reasoning -- see the comments there.
	TagVocabulary []string
	Trip          TripContext
	Locale        string
}

// ExistingPlace is one location already on the trip, as a suggest run needs to
// know it: enough to avoid offering it twice, and nothing more.
type ExistingPlace struct {
	Title string
	// Lat and Lng are nil for a location with no position. Present ones catch
	// the duplicate a name comparison cannot: the same church under a second
	// spelling, or in the other language.
	Lat *float64
	Lng *float64
}

// Suggestions is what a trip-level run produces.
type Suggestions struct {
	// Candidates, in the order the model offered them, after the cap and the
	// dedup. May be empty, which is a legitimate answer.
	Candidates []Candidate

	// Dropped counts candidates removed as duplicates of something already on
	// the trip or of an earlier candidate in the same answer. Reported rather
	// than silently discarded: "it found four and two were places you already
	// have" is a different story from "it only found two", and they want
	// different responses from the user.
	Dropped int

	// Sources the run consulted, for the whole answer rather than per
	// candidate. The agent reads a city guide once and it informs several of
	// them, so attributing pages to individual places would be a provenance
	// trail that is not true.
	Sources []Source
}

// Candidate is one proposed place.
//
// A Location rather than a list of Fields, which is what a Proposal carries:
// there is nothing to diff against, so a before/after pair would have an empty
// before every time.
type Candidate struct {
	Place Location
	// Links that survived the liveness check, exactly as on a Proposal.
	Links []Link
	// Coordinates resolved by internal/geocode from the proposed address, and
	// never values the model produced. Nil when nothing resolved.
	Lat *float64
	Lng *float64
	// Cover is a proposed cover photograph, or nil when none was found.
	Cover *Cover
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

	// Cover is a proposed cover photograph, or nil when none was found.
	Cover *Cover
}

// fieldTags names the one field with set semantics, which two places below
// have to treat differently from the rest.
const fieldTags = "tags"

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
//
// Tags are the exception, and by construction rather than by kindness: a tags
// proposal is the current set plus what the run found, so accepting one adds
// and never removes. Badging that as an overwrite would be false, and a badge
// that cries wolf is worse than no badge. See proposeTags.
func (f Field) Overwrites() bool { return f.Name != fieldTags && f.Current != "" }

// Source is a page the agent read.
type Source struct {
	Title string
	URL   string
	// Image is the page's own og:image, when it advertises one. Carried on the
	// source rather than looked up separately because the page has already
	// been fetched and parsed -- see the note on page.Image in fetch.go.
	Image string
}

// Cover is a proposed cover photograph, with enough provenance to credit it.
//
// Two sources, in priority order. The og:image of a page the agent read is
// preferred: it is the venue's own photograph of itself, and it exists for the
// hotels and restaurants Wikipedia has never heard of. A Wikipedia lead image
// is the fallback, for the landmarks with a good article and no useful
// official site.
//
// Nothing here is downloaded by the agent. This is a suggestion; fetching
// happens if and when somebody accepts it, through the endpoint that already
// stores images.
type Cover struct {
	// URL is the image itself.
	URL string
	// ThumbURL is a smaller rendering when one is known, for the preview.
	// Empty for an og:image, which comes in one size.
	ThumbURL string
	// SourceURL is the page the image came from -- the site whose og:image it
	// is, or the Wikipedia article. Always set: an image with no record of
	// where it came from is a problem waiting for the day somebody shares a
	// trip.
	SourceURL string
	// Credit and Licence are populated for a Wikimedia image and empty for an
	// og:image, which carries no such metadata. Both plain text.
	Credit  string
	Licence string
	// From names the route, for the trace and for the tests: "og" or
	// "wikipedia".
	From string
}

// EventKind separates the three things a run reports.
//
// The zero value is EventProgress, deliberately: every existing construction
// is Event{Key: ...} and means "say this in the status line".
type EventKind string

const (
	// EventProgress is the live status line: what is happening *now*. Fired
	// when a step starts, replaced by the next one, and never accumulated.
	EventProgress EventKind = ""
	// EventStep is one finished step, with how long it took. Fired when a step
	// *ends*, and accumulated by the client into the run trace.
	//
	// A separate kind rather than a duration bolted onto progress, because the
	// two answer different questions and arrive at different moments. A
	// progress event with a duration would either delay the status line until
	// the step finished, or carry a duration of zero.
	EventStep EventKind = "step"
	// EventSummary closes a run: the totals for the trace heading. Last.
	EventSummary EventKind = "summary"
)

// Event is a report from a run in flight.
type Event struct {
	// Kind selects how the client treats this. Zero value is progress.
	Kind EventKind
	// Key is an i18n key ("assist.progress.searching"), not a sentence. The
	// server does not write UI copy: it does not know the user's language,
	// and a translated string arriving over the wire cannot be re-rendered
	// when the user switches locale mid-run.
	Key string
	// Params fills placeholders in the translated string -- the search term,
	// the host being fetched. Values are data, so the client must escape them
	// on render: a page title from a search result is attacker-influenced.
	Params map[string]string

	// DurationMS is how long the step took. EventStep only.
	DurationMS int64
	// Failed marks a step that did not do what it set out to. EventStep only,
	// and not an error: a page that would not load is something the run
	// recovers from, and the trace should say so rather than hide it.
	Failed bool

	// Totals closes the run. EventSummary only.
	Totals Totals
}

// Totals is what a finished run cost, for the trace heading.
type Totals struct {
	DurationMS int64
	Steps      int
	Turns      int
	ToolCalls  int
	// Tokens is zero when the provider reports no usage, which several
	// OpenAI-compatible servers do. The client omits it rather than showing a
	// confident zero.
	Tokens int
}
