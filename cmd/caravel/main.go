package main

import (
	"context"
	"flag"
	"fmt"
	"log"
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
		log.Fatalf("config: %v", err)
	}

	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		log.Fatalf("create upload dir: %v", err)
	}

	dbConn, err := db.Open(cfg.DBDriver, cfg.DBDSN)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer dbConn.Close()

	store, err := db.NewStore(cfg.DBDriver, dbConn)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	authService := auth.NewService(store)
	blob := storagefs.NewLocalFS(cfg.UploadDir)

	// One geocoder, shared: /api/geocode proxies through it and the assistant
	// resolves proposed addresses with it. Nil when unconfigured, which
	// disables both.
	geocoder := geocode.New(cfg.GeocoderURL)

	// Nil when unconfigured, which is not an error: it means the operator did
	// not turn the assistant on. See internal/assist.
	assistant, err := assist.New(assist.Options{
		LLMURL:         cfg.LLMURL,
		LLMKey:         cfg.LLMKey,
		LLMModel:       cfg.LLMModel,
		SearchProvider: cfg.SearchProvider,
		SearchKey:      cfg.SearchKey,
		SearchURL:      cfg.SearchURL,
		Geocoder:       geocoder,
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
		log.Fatalf("%v", err)
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
		log.Printf("assist enabled: %s rate=%d/min/ip concurrent=%d", a.Limits(), rate, concurrent)
	}

	go sweepExpiredSessionsPeriodically(store)

	webFS := httpapi.WebFS(webassets.FS(), cfg.WebDir)
	server := httpapi.NewServer(httpapi.Options{
		DB:       dbConn,
		Store:    store,
		Auth:     authService,
		Blob:     blob,
		WebFS:    webFS,
		NoCache:  cfg.WebDir != "",
		Geocoder: geocoder,
		Assist:   assistant,
		Tiles: httpapi.TileSettings{
			URL:         cfg.TileURL,
			Attribution: cfg.TileAttribution,
			MaxZoom:     cfg.TileMaxZoom,
		},
	})

	log.Printf("caravel %s listening on :%s (db=%s, assist=%t)", buildinfo.Version, cfg.Port, cfg.DBDriver, assistant != nil)
	if err := http.ListenAndServe(":"+cfg.Port, server); err != nil {
		log.Fatalf("server: %v", err)
	}
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
			log.Printf("session sweep: %v", err)
		}
	}
}
