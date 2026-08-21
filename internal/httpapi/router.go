package httpapi

import (
	"database/sql"
	"io/fs"
	"mime"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"caravel/internal/auth"
	"caravel/internal/buildinfo"
	"caravel/internal/db"
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
	// GeocoderURL is the upstream address-search endpoint /api/geocode
	// proxies to. Empty means address search is switched off: the endpoint
	// reports that plainly and the client hides the control.
	GeocoderURL    string
	LoginLimiter   *rateLimiter
	GeocodeLimiter *rateLimiter
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
	// GeocoderURL is the upstream /api/geocode proxies to; empty disables
	// address search.
	GeocoderURL string
}

func NewServer(opts Options) *Server {
	s := &Server{
		DB:          opts.DB,
		Store:       opts.Store,
		Auth:        opts.Auth,
		Blob:        opts.Blob,
		WebFS:       http.FS(opts.WebFS),
		NoCache:     opts.NoCache,
		GeocoderURL: opts.GeocoderURL,
		// 20/minute/IP. Higher than login's 10 because a search is a normal,
		// repeated action rather than a credential attempt, and still far
		// under what would embarrass us upstream.
		LoginLimiter:   newRateLimiter(10, time.Minute),
		GeocodeLimiter: newRateLimiter(20, time.Minute),
	}
	s.router = s.buildRouter()
	go s.sweepLimitersPeriodically()
	return s
}

func (s *Server) sweepLimitersPeriodically() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.LoginLimiter.sweep()
		s.GeocodeLimiter.sweep()
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

				r.Get("/itinerary", s.handleGetItinerary)
				r.Put("/itinerary/days/{date}", s.handleSetItineraryDayNotes)

				r.Put("/preview-image", s.handleSetTripPreviewImage)
				r.Post("/media", s.handleUploadMedia)
				r.Post("/media/url", s.handleCreateMediaURL)

				r.Get("/files", s.handleListTripFiles)
				r.Post("/files", s.handleUploadTripFile)

				r.Get("/checklists", s.handleListChecklists)
				r.Post("/checklists", s.handleCreateChecklist)

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
			r.Delete("/", s.handleDeleteChecklist)
			r.Post("/items", s.handleCreateChecklistItem)
			r.Patch("/items/{itemId}", s.handleSetChecklistItemChecked)
			r.Delete("/items/{itemId}", s.handleDeleteChecklistItem)
		})

		r.Route("/media/{mediaId}", func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Get("/file", s.handleServeMedia)
		})

		r.Route("/itinerary/days/{dayId}", func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Delete("/", s.handleDeleteItineraryDay)
			r.Post("/entries", s.handleCreateItineraryEntry)
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
