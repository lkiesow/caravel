package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	webassets "caravel"
	"caravel/internal/auth"
	"caravel/internal/buildinfo"
	"caravel/internal/config"
	"caravel/internal/db"
	"caravel/internal/httpapi"
	"caravel/internal/storagefs"
)

func main() {
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

	go sweepExpiredSessionsPeriodically(store)

	webFS := httpapi.WebFS(webassets.FS(), cfg.WebDir)
	server := httpapi.NewServer(dbConn, store, authService, blob, webFS, cfg.WebDir != "", cfg.OpenSignup, cfg.GeocoderURL)

	log.Printf("caravel %s listening on :%s (db=%s)", buildinfo.Version, cfg.Port, cfg.DBDriver)
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
