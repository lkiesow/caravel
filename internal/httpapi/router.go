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
	"caravel/internal/db"
	"caravel/internal/storagefs"
)

func init() {
	// Go's mime package doesn't know this extension, so http.FileServer
	// would otherwise serve manifest.webmanifest as text/plain.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

type Server struct {
	DB           *sql.DB
	Store        db.Store
	Auth         *auth.Service
	Blob         storagefs.Blob
	WebFS        http.FileSystem // static assets (embedded, or a live directory in dev)
	NoCache      bool            // true when serving from a live directory (dev mode)
	OpenSignup   bool
	LoginLimiter *loginLimiter
	router       chi.Router
}

func NewServer(dbConn *sql.DB, store db.Store, authService *auth.Service, blob storagefs.Blob, webFS fs.FS, noCache, openSignup bool) *Server {
	s := &Server{
		DB:           dbConn,
		Store:        store,
		Auth:         authService,
		Blob:         blob,
		WebFS:        http.FS(webFS),
		NoCache:      noCache,
		OpenSignup:   openSignup,
		LoginLimiter: newLoginLimiter(10, time.Minute),
	}
	s.router = s.buildRouter()
	go s.sweepLoginLimiterPeriodically()
	return s
}

func (s *Server) sweepLoginLimiterPeriodically() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.LoginLimiter.sweep()
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
			r.With(auth.RequireAuth).Get("/me", s.handleMe)
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

				r.Get("/documents", s.handleListTripDocuments)
				r.Post("/documents", s.handleUploadTripDocument)

				r.Get("/checklists", s.handleListChecklists)
				r.Post("/checklists", s.handleCreateChecklist)
			})
		})

		r.Route("/documents/{docId}", func(r chi.Router) {
			r.Use(auth.RequireAuth)
			r.Delete("/", s.handleDeleteDocument)
			r.Get("/download", s.handleDownloadDocument)
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

			r.Get("/documents", s.handleListItemDocuments)
			r.Post("/documents", s.handleUploadItemDocument)
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

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := s.DB.PingContext(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status":"error"}`))
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}
