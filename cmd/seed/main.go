// Command seed populates a Caravel database with a demo user and a sample
// trip (a few items, a day, an itinerary entry) so local development and
// manual testing don't have to start from an empty database every time.
// Not part of the served application — run manually via `make dev-seed`.
package main

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"caravel/internal/auth"
	"caravel/internal/config"
	"caravel/internal/db"
)

const (
	demoUsername    = "demo"
	demoPassword    = "demo1234"
	demoDisplayName = "Demo User"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
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

	ctx := context.Background()
	authService := auth.NewService(store)

	user, err := authService.Register(ctx, demoUsername, demoPassword, demoDisplayName)
	if err != nil {
		if err == auth.ErrUsernameTaken {
			user, err = store.GetUserByUsername(ctx, demoUsername)
			if err != nil {
				log.Fatalf("look up existing demo user: %v", err)
			}
		} else {
			log.Fatalf("register demo user: %v", err)
		}
	} else {
		log.Printf("created demo user %q (password: %s)", demoUsername, demoPassword)
	}

	if err := seedTrip(ctx, store, user.ID); err != nil {
		log.Fatalf("seed trip: %v", err)
	}
}

func seedTrip(ctx context.Context, store db.Store, ownerID string) error {
	now := time.Now().UTC()
	start := now.AddDate(0, 0, 7).Format("2006-01-02")
	end := now.AddDate(0, 0, 10).Format("2006-01-02")
	subtitle := "A short demo trip seeded for local testing — nothing here is real."

	trip, err := store.CreateTrip(ctx, db.CreateTripParams{
		ID:        uuid.NewString(),
		OwnerID:   ownerID,
		Title:     "Demo Trip: Iceland Ring Road",
		StartDate: &start,
		EndDate:   &end,
		Subtitle:  &subtitle,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return err
	}

	items := []db.CreateItemParams{
		{ID: uuid.NewString(), TripID: trip.ID, Category: "site", Type: "landmark", Title: "Kirkjufell", SortOrder: 0, CreatedAt: now, UpdatedAt: now},
		{ID: uuid.NewString(), TripID: trip.ID, Category: "stay", Type: "hotel", Title: "Foss Hotel Reykjavik", SortOrder: 1, CreatedAt: now, UpdatedAt: now},
		{ID: uuid.NewString(), TripID: trip.ID, Category: "transport", Type: "flight", Title: "Flight to Keflavik", SortOrder: 2, CreatedAt: now, UpdatedAt: now},
	}
	var firstItemID string
	for i, p := range items {
		created, err := store.CreateItem(ctx, p)
		if err != nil {
			return err
		}
		if i == 0 {
			firstItemID = created.ID
		}
	}

	day, err := store.UpsertItineraryDayNotes(ctx, uuid.NewString(), trip.ID, start, nil)
	if err != nil {
		return err
	}
	if _, err := store.CreateItineraryEntry(ctx, db.CreateItineraryEntryParams{
		ID:             uuid.NewString(),
		ItineraryDayID: day.ID,
		ItemID:         firstItemID,
		SortOrder:      0,
	}); err != nil {
		return err
	}

	log.Printf("seeded trip %q (id=%s) with %d items for user %s", trip.Title, trip.ID, len(items), ownerID)
	return nil
}
