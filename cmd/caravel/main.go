package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	webassets "caravel"
	"caravel/internal/assist"
	"caravel/internal/auth"
	"caravel/internal/buildinfo"
	"caravel/internal/config"
	"caravel/internal/db"
	"caravel/internal/geocode"
	"caravel/internal/httpapi"
	"caravel/internal/storagefs"
	"caravel/internal/wikimedia"
)

// health is the container's HEALTHCHECK, and the reason it lives in this binary
// rather than in a shell command: the image is distroless, so there is no curl,
// no wget and no shell to run them from. `caravel -health` asks the server on
// this host whether it is up, which is exactly what an external check would do.
//
// Deliberately does not go through config.Load: a health check that fails
// because some unrelated variable is malformed would report the wrong thing,
// and the only setting it needs is the port.
func health() int {
	port := os.Getenv("CARAVEL_PORT")
	if port == "" {
		port = "8080"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/api/health")
	if err != nil {
		fmt.Fprintf(os.Stderr, "health: %v\n", err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// /api/health answers 503 when the database ping fails, which is a
		// server that is listening but cannot serve - unhealthy, not healthy.
		fmt.Fprintf(os.Stderr, "health: HTTP %d\n", resp.StatusCode)
		return 1
	}
	return 0
}

func main() {
	healthOnly := flag.Bool("health", false, "check whether the server on this host is healthy, then exit (used by the container HEALTHCHECK)")
	flag.Parse()
	if *healthOnly {
		os.Exit(health())
	}

	cfg, err := config.Load()
	if err != nil {
		// Before the logger exists, and deliberately: a configuration failure
		// includes a malformed CARAVEL_LOG_LEVEL, so there is nothing to
		// install yet and stderr is the only honest place for this.
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	// Installed as the default so every package can reach it with slog.Default
	// rather than being handed a logger through five constructors. See
	// setupLogging for what the two settings do.
	setupLogging(cfg)

	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		fatal("create upload dir", err)
	}

	dbConn, err := db.Open(cfg.DBDriver, cfg.DBDSN)
	if err != nil {
		fatal("db", err)
	}
	defer dbConn.Close()

	store, err := db.NewStore(cfg.DBDriver, dbConn)
	if err != nil {
		fatal("store", err)
	}
	authService := auth.NewService(store)
	blob := storagefs.NewLocalFS(cfg.UploadDir)

	// One geocoder, shared: /api/geocode proxies through it and the assistant
	// resolves proposed addresses with it. Nil when unconfigured, which
	// disables both.
	geocoder := geocode.New(cfg.GeocoderURL)

	// One web-search backend, shared: the assistant researches with it and
	// the image picker searches with it. Built here rather than inside
	// assist.New because since Stage 21 Milestone 7 it has two consumers and
	// is no longer the assistant's private dependency -- an instance may
	// configure a search provider with no LLM at all.
	searcher, err := assist.NewSearcher(cfg.SearchProvider, cfg.SearchKey, cfg.SearchURL)
	if err != nil {
		fatal("search", err)
	}

	// Wikipedia needs no configuration and no key, which is what makes the
	// image picker work on a stock instance.
	wiki := wikimedia.New(cfg.WikimediaURL)

	// Nil when unconfigured, which is not an error: it means the operator did
	// not turn the assistant on. See internal/assist.
	assistant, err := assist.New(assist.Options{
		LLMURL:   cfg.LLMURL,
		LLMKey:   cfg.LLMKey,
		LLMModel: cfg.LLMModel,
		Searcher: searcher,
		Geocoder: geocoder,
		// The same endpoint the image picker uses, so an instance pinned to a
		// mirror is pinned for both.
		WikimediaURL: cfg.WikimediaURL,
		Limits: assist.Limits{
			RunDuration:   cfg.AssistTimeout,
			AnswerTimeout: cfg.AssistAnswerTimeout,
			MaxTurns:      cfg.AssistMaxTurns,
			MaxToolCalls:  cfg.AssistMaxToolCalls,
			MaxTokens:     cfg.AssistMaxTokens,
			AnswerReserve: cfg.AssistAnswerReserve,
		},
	})
	if err != nil {
		fatal("assist", err)
	}
	// Logged rather than left implicit: these bound what the instance can
	// spend, and "what is this server actually running with" should be
	// answerable from the log rather than by reading a running process's
	// environment.
	if a, ok := assistant.(*assist.Agent); ok {
		rate := cfg.AssistRateLimit
		if rate <= 0 {
			rate = httpapi.DefaultAssistRateLimit
		}
		concurrent := cfg.AssistMaxConcurrent
		if concurrent <= 0 {
			concurrent = httpapi.DefaultAssistMaxConcurrent
		}
		slog.Info("assist enabled",
			"limits", a.Limits().String(),
			"rate_per_min_per_ip", rate,
			"max_concurrent", concurrent,
			"search", cfg.SearchProvider)
	}

	go sweepExpiredSessionsPeriodically(store)

	webFS := httpapi.WebFS(webassets.FS(), cfg.WebDir)
	server := httpapi.NewServer(httpapi.Options{
		DB:        dbConn,
		Store:     store,
		Auth:      authService,
		Blob:      blob,
		WebFS:     webFS,
		NoCache:   cfg.WebDir != "",
		Geocoder:  geocoder,
		Assist:    assistant,
		Wikimedia: wiki,
		Searcher:  searcher,
		Tiles: httpapi.TileSettings{
			URL:         cfg.TileURL,
			Attribution: cfg.TileAttribution,
			MaxZoom:     cfg.TileMaxZoom,
		},
	})

	slog.Info("caravel listening",
		"version", buildinfo.Version,
		"port", cfg.Port,
		"db", cfg.DBDriver,
		"assist", assistant != nil,
		// Reported separately from "assist" because since Milestone 7 the two
		// are separable: the image picker works with no LLM, and its
		// Wikipedia half works with nothing configured at all. What is worth
		// logging is therefore the half that *is* conditional -- whether the
		// configured search backend can also search for images.
		"image_search_web", webImageSearch(searcher))
	if err := http.ListenAndServe(":"+cfg.Port, server); err != nil {
		fatal("server", err)
	}
}

// webImageSearch names the backend behind the web half of the image picker,
// or says why there is none. A string rather than a bool because "configured
// but cannot do images" is the answer an operator is most likely to be
// surprised by, and it is invisible in a true/false.
func webImageSearch(searcher assist.Searcher) string {
	if searcher == nil {
		return "none"
	}
	if _, ok := searcher.(assist.ImageSearcher); !ok {
		return searcher.Name() + " (no image search)"
	}
	return searcher.Name()
}

// setupLogging installs the process-wide logger.
//
// Text by default because a self-hosted instance's log is read by a person in
// journalctl or `docker logs`, where one line per record beats a wall of JSON.
// CARAVEL_LOG_FORMAT=json is there for anyone shipping to something that
// parses.
//
// The level is what makes the assistant's run trace reachable: at info the
// server says what it is doing, at debug internal/assist accounts for every
// turn, every tool call and where the time went. See the note on logging in
// that package for what must never appear in it.
func setupLogging(cfg config.Config) {
	opts := &slog.HandlerOptions{Level: cfg.LogLevel}
	var handler slog.Handler
	if cfg.LogFormat == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(handler))
}

// fatal replaces log.Fatalf: the same "say why and stop", through the logger
// the operator configured rather than around it.
func fatal(what string, err error) {
	slog.Error("caravel: cannot start", "at", what, "err", err)
	os.Exit(1)
}

// sweepExpiredSessionsPeriodically deletes expired session rows so the
// table doesn't grow unbounded on a long-running instance. Sessions already
// stop authenticating once expired (see auth.Service.ValidateSession); this
// is just cleanup.
func sweepExpiredSessionsPeriodically(store db.Store) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		if err := store.DeleteExpiredSessions(context.Background(), time.Now().UTC()); err != nil {
			slog.Warn("session sweep failed", "err", err)
		}
	}
}
