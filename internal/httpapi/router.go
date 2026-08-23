package httpapi

import (
	"database/sql"
	"io/fs"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"caravel/internal/assist"
	"caravel/internal/auth"
	"caravel/internal/buildinfo"
	"caravel/internal/db"
	"caravel/internal/geocode"
	"caravel/internal/storagefs"
)

func init() {
	// Go's mime package doesn't know this extension, so http.FileServer
	// would otherwise serve manifest.webmanifest as text/plain.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

type Server struct {
	DB      *sql.DB
	Store   db.Store
	Auth    *auth.Service
	Blob    storagefs.Blob
	WebFS   http.FileSystem // static assets (embedded, or a live directory in dev)
	NoCache bool            // true when serving from a live directory (dev mode)
	// Geocoder resolves place names to coordinates for /api/geocode. Nil
	// means address search is switched off: the endpoint reports that plainly
	// and the client hides the control. Shared with Assist below, which
	// resolves the addresses the model proposes through the same client.
	Geocoder *geocode.Client
	// Assist proposes location metadata via an LLM. Nil means the feature is
	// not configured, which is the only off switch: the route answers 501,
	// /auth/me reports the capability as absent and the client hides the
	// control. Same shape as GeocoderURL above.
	Assist assist.Assistant
	// assistSlots is a counting semaphore over in-flight assist runs; see
	// DefaultAssistMaxConcurrent. A buffered channel rather than a mutex and
	// a counter, so the non-blocking "is there room" question is one select.
	assistSlots    chan struct{}
	LoginLimiter   *rateLimiter
	GeocodeLimiter *rateLimiter
	AssistLimiter  *rateLimiter
	router         chi.Router
}

// Options are NewServer's dependencies and settings.
//
// A struct rather than positional parameters, as of Stage 14 Milestone 5. The
// signature had reached eight arguments of which four were bare bools and
// strings, so `NewServer(..., false, true, "")` said nothing at either call
// site about which flag was which — and this milestone was removing one of
// them, which is the moment to stop widening it. Whether registration is open
// is deliberately absent: it lives in app_settings, where an admin can change
// it (see settings.go).
type Options struct {
	DB    *sql.DB
	Store db.Store
	Auth  *auth.Service
	Blob  storagefs.Blob
	// WebFS is the static asset tree: the embedded copy, or a live directory
	// in dev.
	WebFS fs.FS
	// NoCache disables asset caching, for serving from a live directory.
	NoCache bool
	// Geocoder backs /api/geocode; nil disables address search.
	Geocoder *geocode.Client
	// Assist is the location-metadata assistant, or nil when unconfigured.
	Assist assist.Assistant
	// AssistRateLimit is runs per minute per client address. Zero takes
	// defaultAssistRateLimit. This is the only thing bounding how *many* runs
	// happen -- assist.Limits bounds what one run may spend -- so the
	// worst-case cost of an instance is roughly the two multiplied together.
	AssistRateLimit int
	// AssistMaxConcurrent bounds how many runs may be in flight at once. Zero
	// takes DefaultAssistMaxConcurrent.
	AssistMaxConcurrent int
}

// DefaultAssistRateLimit is far tighter than the other limiters because the
// budget being protected is different in kind: a login attempt costs a hash
// and a geocode costs somebody else a cheap lookup, but one assist run is a
// multi-turn LLM conversation the instance owner pays for by the token. Six a
// minute is generous for a human filling in a form and ungenerous for anything
// else.
// Exported so the startup log can report the *effective* value rather than
// the configured one, which is zero whenever the operator left it alone.
const DefaultAssistRateLimit = 6

// DefaultAssistMaxConcurrent bounds runs in flight at once.
//
// The rate limiter above bounds how often runs *start*, per client address.
// This bounds how many are alive across the whole instance, which is the
// number that actually decides the worst-case bill and the worst-case load:
// without it, ten browser tabs are ten simultaneous multi-turn conversations
// against a metered API, and the per-IP limiter does not see them as related.
//
// Four rather than one, because a household or a small group planning
// together should not queue behind each other, and rather than twenty because
// this is a self-hosted app for a handful of people and the failure it guards
// is financial.
const DefaultAssistMaxConcurrent = 4

func NewServer(opts Options) *Server {
	s := &Server{
		DB:       opts.DB,
		Store:    opts.Store,
		Auth:     opts.Auth,
		Blob:     opts.Blob,
		WebFS:    http.FS(opts.WebFS),
		NoCache:  opts.NoCache,
		Geocoder: opts.Geocoder,
		Assist:   opts.Assist,
		// 20/minute/IP. Higher than login's 10 because a search is a normal,
		// repeated action rather than a credential attempt, and still far
		// under what would embarrass us upstream.
		LoginLimiter:   newRateLimiter(10, time.Minute),
		GeocodeLimiter: newRateLimiter(20, time.Minute),
		AssistLimiter:  newRateLimiter(assistRateLimit(opts.AssistRateLimit), time.Minute),
		assistSlots:    make(chan struct{}, assistMaxConcurrent(opts.AssistMaxConcurrent)),
	}
	s.router = s.buildRouter()
	go s.sweepLimitersPeriodically()
	return s
}

func assistRateLimit(configured int) int {
	if configured <= 0 {
		return DefaultAssistRateLimit
	}
	return configured
}

func assistMaxConcurrent(configured int) int {
	if configured <= 0 {
		return DefaultAssistMaxConcurrent
	}
	return configured
}

// acquireAssistSlot takes a slot if one is free, returning a release function.
// Never blocks: a request that waited behind three others would only time out
// further downstream, and refusing lets the client decide whether to retry.
func (s *Server) acquireAssistSlot() (func(), bool) {
	select {
	case s.assistSlots <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-s.assistSlots }) }, true
	default:
		return nil, false
	}
}

func (s *Server) sweepLimitersPeriodically() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.LoginLimiter.sweep()
		s.GeocodeLimiter.sweep()
		s.AssistLimiter.sweep()
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	r.Use(middleware.Compress(5))
	r.Use(s.Auth.Middleware)

	r.Route("/api", func(r chi.Router) {
		r.Get("/health", s.handleHealth)

		r.Route("/auth", func(r chi.Router) {
			r.With(s.rateLimitLogin).Post("/register", s.handleRegister)
			r.With(s.rateLimitLogin).Post("/login", s.handleLogin)
			r.Post("/logout", s.handleLogout)
			// Unauthenticated: the login page needs this before anyone has a
			// session. See handleAuthConfig for why publishing it is fine.
			r.Get("/config", s.handleAuthConfig)
			r.With(auth.RequireAuth).Get("/me", s.handleMe)
			// Rate limited like login for the same reason: it takes the current
			// password, so it is another place to guess one.
			r.With(auth.RequireAuth, s.rateLimitLogin).Post("/password", s.handleChangePassword)
		})

		// Not trip-scoped: the Members tab searches for someone who is by
		// definition not on the trip yet. Filtering out people already on it
		// happens on the client, which already has the member list.
		r.With(auth.RequireAuth).Get("/users/search", s.handleSearchUsers)

		// Not trip-scoped either: it renders the text sitting in the caller's
		// own textarea, so there is nothing to authorize against but the
		// session. See internal/httpapi/markdown.go.
		r.With(auth.RequireAuth).Post("/markdown/preview", s.handleMarkdownPreview)

		// Behind RequireAuth as well as its own limiter: this endpoint spends
		// an external service's quota, so it is not for anonymous callers.
		r.With(auth.RequireAuth, s.rateLimitGeocode).Get("/geocode", s.handleGeocode)

		// Account administration. requireAdmin sits inside RequireAuth: an
		// anonymous caller gets 401 and a logged-in non-admin 403, which are
		// genuinely different situations for a client to react to.
		r.Route("/admin", func(r chi.Router) {
			r.Use(auth.RequireAuth, requireAdmin)
			r.Get("/users", s.handleAdminListUsers)
			r.Post("/users", s.handleAdminCreateUser)
			r.Patch("/users/{userId}", s.handleAdminUpdateUser)
			r.Post("/users/{userId}/password", s.handleAdminResetPassword)
			r.Delete("/users/{userId}", s.handleAdminDeleteUser)
			r.Put("/settings/open-signup", s.handleAdminSetOpenSignup)
		})

		r.Route("/trips", func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Get("/", s.handleListTrips)
			r.Post("/", s.handleCreateTrip)
			r.Route("/{tripId}", func(r chi.Router) {
				r.Get("/", s.handleGetTrip)
				r.Patch("/", s.handleUpdateTrip)
				r.Delete("/", s.handleDeleteTrip)

				r.Get("/items", s.handleListItems)
				r.Post("/items", s.handleCreateItem)

				r.Get("/map", s.handleGetTripMap)

				// Editor, not viewer: a viewer could not save the result, and
				// the request may carry the trip title and dates outward to a
				// third party. Its own limiter -- see AssistLimiter.
				r.With(s.rateLimitAssist).Post("/assist/location", s.handleAssistLocation)

				r.Get("/itinerary", s.handleGetItinerary)
				r.Put("/itinerary/days/{date}", s.handleSetItineraryDayNotes)

				r.Put("/preview-image", s.handleSetTripPreviewImage)
				r.Post("/media", s.handleUploadMedia)
				r.Post("/media/url", s.handleCreateMediaURL)

				r.Get("/files", s.handleListTripFiles)
				r.Post("/files", s.handleUploadTripFile)

				r.Get("/checklists", s.handleListChecklists)
				r.Post("/checklists", s.handleCreateChecklist)

				// Reading the ledger needs only viewer: everyone on a trip may
				// see what it cost, including someone who cannot change it.
				r.Get("/expenses", s.handleListExpenses)
				r.Post("/expenses", s.handleCreateExpense)

				// Reading the member list needs only viewer: everyone on a
				// trip may see who else is on it. The writes are owner-only,
				// except that handleRemoveMember also serves "leave trip" and
				// so does its own check.
				r.Get("/members", s.handleListMembers)
				r.Post("/members", s.handleAddMember)
				r.Put("/members/{userId}", s.handleSetMemberRole)
				r.Delete("/members/{userId}", s.handleRemoveMember)
			})
		})

		r.Route("/files/{fileId}", func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Patch("/", s.handleUpdateFileNote)
			r.Delete("/", s.handleDeleteFile)
			r.Put("/visibility", s.handleSetFileVisibility)
			r.Get("/download", s.handleDownloadFile)
		})

		r.Route("/checklists/{checklistId}", func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Patch("/", s.handleRenameChecklist)
			r.Delete("/", s.handleDeleteChecklist)
			r.Put("/visibility", s.handleSetChecklistVisibility)
			// A create, not a mutation of the source, so it sits here rather
			// than under the trip: the id in the path says what to copy.
			r.Post("/duplicate", s.handleDuplicateChecklist)
			r.Post("/items", s.handleCreateChecklistItem)
			r.Patch("/items/{itemId}", s.handleSetChecklistItemChecked)
			// Separate from the PATCH above, which carries `checked`: ticking is
			// a different action from rewording, and one endpoint taking either
			// would make an absent field ambiguous.
			r.Put("/items/{itemId}/text", s.handleUpdateChecklistItemText)
			r.Delete("/items/{itemId}", s.handleDeleteChecklistItem)
		})

		r.Route("/expenses/{expenseId}", func(r chi.Router) {
			r.Use(auth.RequireAuth)
			// No visibility route and no per-field endpoints: an expense is
			// four fields that are edited together, so PATCH carries all of
			// them. See the note on expenseRequest for what an absent payer
			// means, which differs between create and update.
			r.Patch("/", s.handleUpdateExpense)
			r.Delete("/", s.handleDeleteExpense)
		})

		r.Route("/media/{mediaId}", func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Get("/file", s.handleServeMedia)
		})

		r.Route("/itinerary/days/{dayId}", func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Delete("/", s.handleDeleteItineraryDay)
			r.Post("/entries", s.handleCreateItineraryEntry)
			// PUT on the collection, not PATCH on an entry: reordering is one
			// statement about the whole day, and the body is every entry id in
			// the order they should end up in.
			r.Put("/entries/order", s.handleReorderItineraryEntries)
			r.Delete("/entries/{entryId}", s.handleDeleteItineraryEntry)
		})

		r.Route("/items/{itemId}", func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Get("/", s.handleGetItem)
			r.Patch("/", s.handleUpdateItem)
			r.Delete("/", s.handleDeleteItem)

			r.Put("/location", s.handlePutItemLocation)
			r.Put("/image", s.handleSetItemImage)

			r.Post("/links", s.handleCreateItemLink)
			r.Delete("/links/{linkId}", s.handleDeleteItemLink)

			r.Post("/dates", s.handleCreateItemDate)
			r.Delete("/dates/{dateId}", s.handleDeleteItemDate)

			r.Get("/files", s.handleListItemFiles)
			r.Post("/files", s.handleUploadItemFile)
		})
	})

	fileServer := http.FileServer(s.WebFS)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if s.NoCache {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		}
		// SPA fallback: serve index.html for any non-API path so client-side
		// routing (History API) works on a hard refresh/deep link.
		if _, err := s.WebFS.Open(r.URL.Path); err != nil {
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})

	return r
}

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// The version is reported here as well as in the startup banner so a test, a
// deploy script or a person can ask the *running* server what it is, without
// access to its logs — the banner is only visible to whoever started it. This
// endpoint is unauthenticated, so the SHA is public; for a self-hosted app
// whose source is public anyway that is information, not a secret.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.DB.PingContext(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "error", Version: buildinfo.Version})
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok", Version: buildinfo.Version})
}
