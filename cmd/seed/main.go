// Command seed populates a Caravel database with a demo user and one or more
// named demo trips, so local development and manual testing don't have to start
// from an empty database — or, worse, from a database full of half-deleted
// leftovers from the last round of testing.
//
// Every Stage 07 milestone needed a *specific* trip shape (no dates, only a
// start date, exactly one mappable location, days outside the trip's range…)
// and built it through ad-hoc fetch calls against a running server, then
// hand-deleted the results imperfectly. Those shapes are scenarios here
// instead. `make dev-reset` wipes and reseeds, which removes the cleanup half
// of that problem entirely.
//
// Run via `make dev-seed` (all scenarios) or `make dev-seed SCENARIO=one-pin`.
// Not part of the served application.
package main

import (
	"bytes"
	"context"
	"embed"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"caravel/internal/auth"
	"caravel/internal/config"
	"caravel/internal/db"
	"caravel/internal/imaging"
	"caravel/internal/storagefs"
)

// Embedded rather than read from disk, so `go run ./cmd/seed` works from any
// working directory and a built seed binary needs no companion files — the same
// reason internal/db embeds its migrations.
//
//go:embed images/*.jpg
var fixtureImages embed.FS

const (
	demoUsername    = "demo"
	demoPassword    = "demo1234"
	demoDisplayName = "Demo User"

	// A second account, so cross-user ownership checks have someone to be
	// "another user" without inventing one by hand each time.
	otherUsername    = "other"
	otherPassword    = "other1234"
	otherDisplayName = "Other User"

	// Every seeded title starts with this, so a UI test can find its trip by
	// name and a human can tell seeded data from anything they created.
	titlePrefix = "Demo: "
)

// Fixed date the deterministic scenarios are built around. Real dates would
// make every seeded ID and every date-dependent assertion move day to day; the
// `full` scenario is the deliberate exception (see below).
var baseDate = time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC)

// Namespace for deterministic v5 UUIDs. Seeded rows get the *same* IDs on every
// run, so a UI test can hard-code /trips/<id>/locations instead of looking the
// trip up first, and re-seeding replaces a scenario in place rather than piling
// up duplicates.
var seedNamespace = uuid.MustParse("6f9619ff-8b86-d011-b42d-00c04fc964ff")

func seedID(parts ...string) string {
	return uuid.NewSHA1(seedNamespace, []byte(strings.Join(parts, "/"))).String()
}

func day(base time.Time, offset int) string {
	return base.AddDate(0, 0, offset).Format("2006-01-02")
}

func ptr[T any](v T) *T { return &v }

type seedCtx struct {
	ctx     context.Context
	store   db.Store
	blob    storagefs.Blob
	ownerID string
	otherID string
}

type scenario struct {
	name string
	desc string
	run  func(s seedCtx) error
}

// Ordered so `make dev-seed` produces a stable trips list.
var scenarios = []scenario{
	{"full", "everything populated: locations with coordinates on the map, itinerary, checklist, documents", seedFull},
	{"one-pin", "exactly one mappable location — the zero-size map bounds case", seedOnePin},
	{"start-only", "a start date but no end date", seedStartOnly},
	{"year-boundary", "a trip crossing 31 December", seedYearBoundary},
	{"no-dates", "neither start nor end date set", seedNoDates},
	{"out-of-range-days", "itinerary days outside the trip's own date range", seedOutOfRangeDays},
	{"cascade", "a trip with children of every kind, for delete-cascade checks", seedCascade},
}

func main() {
	var only string
	flag.StringVar(&only, "scenario", "", "seed only this scenario (default: all)")
	flag.Parse()

	if only != "" && only != "all" && findScenario(only) == nil {
		fmt.Fprintf(os.Stderr, "seed: unknown scenario %q\n\navailable:\n", only)
		for _, sc := range scenarios {
			fmt.Fprintf(os.Stderr, "  %-18s %s\n", sc.name, sc.desc)
		}
		os.Exit(2)
	}

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

	owner, err := ensureUser(ctx, store, authService, demoUsername, demoPassword, demoDisplayName)
	if err != nil {
		log.Fatalf("demo user: %v", err)
	}
	other, err := ensureUser(ctx, store, authService, otherUsername, otherPassword, otherDisplayName)
	if err != nil {
		log.Fatalf("other user: %v", err)
	}

	s := seedCtx{
		ctx:     ctx,
		store:   store,
		blob:    storagefs.NewLocalFS(cfg.UploadDir),
		ownerID: owner.ID,
		otherID: other.ID,
	}

	selected := scenarios
	if sc := findScenario(only); sc != nil {
		selected = []scenario{*sc}
	}

	for _, sc := range selected {
		if err := sc.run(s); err != nil {
			log.Fatalf("scenario %s: %v", sc.name, err)
		}
		log.Printf("seeded %-18s %s", sc.name, sc.desc)
	}
	log.Printf("done — %d scenario(s) for user %q (password: %s)", len(selected), demoUsername, demoPassword)
}

func findScenario(name string) *scenario {
	for i := range scenarios {
		if scenarios[i].name == name {
			return &scenarios[i]
		}
	}
	return nil
}

func ensureUser(ctx context.Context, store db.Store, authService *auth.Service, username, password, displayName string) (db.User, error) {
	user, err := authService.Register(ctx, username, password, displayName)
	if err == nil {
		log.Printf("created user %q (password: %s)", username, password)
		return user, nil
	}
	if err != auth.ErrUsernameTaken {
		return db.User{}, err
	}
	return store.GetUserByUsername(ctx, username)
}

// newTrip deletes any previous incarnation of this scenario's trip before
// creating it, which is what makes re-running the seed idempotent: the trip's ID
// is deterministic, so without the delete a second run would collide on the
// primary key. DeleteTrip cascades to items, days, entries, checklists and
// documents, so this clears the whole scenario.
func (s seedCtx) newTrip(scenarioName, title string, start, end *string, subtitle string) (db.Trip, error) {
	id := seedID(scenarioName, "trip")
	if _, err := s.store.DeleteTrip(s.ctx, id, s.ownerID); err != nil {
		return db.Trip{}, fmt.Errorf("clear previous %s trip: %w", scenarioName, err)
	}
	now := time.Now().UTC()
	return s.store.CreateTrip(s.ctx, db.CreateTripParams{
		ID:        id,
		OwnerID:   s.ownerID,
		Title:     titlePrefix + title,
		StartDate: start,
		EndDate:   end,
		Subtitle:  ptr(subtitle),
		CreatedAt: now,
		UpdatedAt: now,
	})
}

type linkSpec struct {
	url   string
	label string
}

type dateSpec struct {
	start string
	end   string
	label string
}

type itemSpec struct {
	key      string // stable per-trip identity, for the deterministic ID
	category string
	itemType string
	title    string
	notes    string
	lat, lng *float64 // nil = no coordinates at all
	onMap    bool
	links    []linkSpec
	dates    []dateSpec
}

func (s seedCtx) addItems(scenarioName, tripID string, specs []itemSpec) ([]db.Item, error) {
	now := time.Now().UTC()
	items := make([]db.Item, 0, len(specs))
	for i, spec := range specs {
		itemID := seedID(scenarioName, "item", spec.key)
		item, err := s.store.CreateItem(s.ctx, db.CreateItemParams{
			ID:        itemID,
			TripID:    tripID,
			Category:  spec.category,
			Type:      spec.itemType,
			Title:     spec.title,
			Notes:     ptr(spec.notes),
			ShowOnMap: spec.onMap,
			SortOrder: i,
			CreatedAt: now,
			UpdatedAt: now,
		})
		if err != nil {
			return nil, fmt.Errorf("create item %s: %w", spec.key, err)
		}
		if spec.lat != nil && spec.lng != nil {
			if _, err := s.store.UpsertItemLocation(s.ctx, db.UpsertItemLocationParams{
				ID:     seedID(scenarioName, "location", spec.key),
				ItemID: itemID,
				Lat:    spec.lat,
				Lng:    spec.lng,
			}); err != nil {
				return nil, fmt.Errorf("set location for %s: %w", spec.key, err)
			}
		}
		for j, l := range spec.links {
			if _, err := s.store.CreateItemLink(s.ctx, db.CreateItemLinkParams{
				ID:        seedID(scenarioName, "link", spec.key, l.url),
				ItemID:    itemID,
				URL:       l.url,
				Label:     ptr(l.label),
				SortOrder: j,
			}); err != nil {
				return nil, fmt.Errorf("add link for %s: %w", spec.key, err)
			}
		}
		for _, d := range spec.dates {
			if _, err := s.store.CreateItemDate(s.ctx, db.CreateItemDateParams{
				ID:        seedID(scenarioName, "date", spec.key, d.start),
				ItemID:    itemID,
				StartDate: ptr(d.start),
				EndDate:   ptr(d.end),
				Label:     ptr(d.label),
				AllDay:    true,
			}); err != nil {
				return nil, fmt.Errorf("add date for %s: %w", spec.key, err)
			}
		}
		items = append(items, item)
	}
	return items, nil
}

func (s seedCtx) addDay(scenarioName, tripID, date string, notes *string) (db.ItineraryDay, error) {
	return s.store.UpsertItineraryDayNotes(s.ctx, seedID(scenarioName, "day", date), tripID, date, notes)
}

func (s seedCtx) addEntry(scenarioName, dayID, itemID string, sortOrder int) error {
	_, err := s.store.CreateItineraryEntry(s.ctx, db.CreateItineraryEntryParams{
		ID:             seedID(scenarioName, "entry", dayID, itemID),
		ItineraryDayID: dayID,
		ItemID:         itemID,
		SortOrder:      sortOrder,
	})
	return err
}

func (s seedCtx) addChecklist(scenarioName, tripID, title string, items []string) error {
	list, err := s.store.CreateChecklist(s.ctx, db.CreateChecklistParams{
		ID:        seedID(scenarioName, "checklist", title),
		TripID:    tripID,
		Title:     title,
		SortOrder: 0,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	for i, text := range items {
		if _, err := s.store.CreateChecklistItem(s.ctx, db.CreateChecklistItemParams{
			ID:          seedID(scenarioName, "checklistItem", title, text),
			ChecklistID: list.ID,
			Text:        text,
			Checked:     i == 0, // one ticked, so the checked state is visible
			SortOrder:   i,
			CreatedAt:   time.Now().UTC(),
		}); err != nil {
			return err
		}
	}
	return nil
}

// addImage stores one of the embedded fixture images as a media asset and
// returns its ID, ready to hand to SetTripPreviewImage or SetItemImage.
//
// It goes through imaging.DecodeAndResize — the same call handleUploadMedia
// makes — rather than copying the bytes straight to the blob store, so a
// seeded asset is byte-for-byte what an upload of the same file would have
// produced, content type and dimensions included. Otherwise the seed would be
// exercising a path no real upload takes.
//
// Why any of this exists: no scenario set a trip cover photo or an item image,
// so .image-field__preview, .itinerary-entry__thumb and the location card's
// thumbnail rendered their empty state in every UI sweep and were never
// measured (see todo.md). The fixtures are small (~343x200) crops of a test
// sheet, deliberately: they are test data, so softness when a 640px-wide banner
// scales one up is expected and is not a layout bug.
func (s seedCtx) addImage(scenarioName, tripID, filename string) (string, error) {
	data, err := fixtureImages.ReadFile("images/" + filename)
	if err != nil {
		return "", fmt.Errorf("read fixture %s: %w", filename, err)
	}
	result, err := imaging.DecodeAndResize(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("decode fixture %s: %w", filename, err)
	}

	id := seedID(scenarioName, "image", filename)
	key := fmt.Sprintf("%s/images/%s.jpg", tripID, id)
	if _, err := s.blob.Put(s.ctx, key, bytes.NewReader(result.Data)); err != nil {
		return "", fmt.Errorf("store blob for %s: %w", filename, err)
	}
	if _, err := s.store.CreateMediaAsset(s.ctx, db.CreateMediaAssetParams{
		ID:          id,
		TripID:      tripID,
		Kind:        "upload",
		StoragePath: &key,
		ContentType: &result.ContentType,
		Width:       &result.Width,
		Height:      &result.Height,
		CreatedAt:   time.Now().UTC(),
	}); err != nil {
		return "", fmt.Errorf("create media asset for %s: %w", filename, err)
	}
	return id, nil
}

// addDocument writes a real (tiny) file through the blob store as well as the
// row, so the Documents tab has something that actually downloads.
func (s seedCtx) addDocument(scenarioName, tripID string, itemID *string, filename, body string) error {
	id := seedID(scenarioName, "document", filename)
	key := fmt.Sprintf("%s/%s", tripID, id)
	size, err := s.blob.Put(s.ctx, key, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("store blob for %s: %w", filename, err)
	}
	_, err = s.store.CreateDocument(s.ctx, db.CreateDocumentParams{
		ID:          id,
		TripID:      tripID,
		ItemID:      itemID,
		Filename:    filename,
		StoragePath: key,
		ContentType: ptr("text/plain; charset=utf-8"),
		SizeBytes:   size,
		UploadedAt:  time.Now().UTC(),
		Note:        ptr("Seeded document"),
	})
	return err
}

// --- scenarios -------------------------------------------------------------

// seedFull is the general-purpose demo trip. Its dates are deliberately
// relative to today rather than to baseDate, so the default trip always looks
// like an upcoming one; every other scenario is fully deterministic.
//
// Note the coordinates and ShowOnMap: true. The previous seed set neither, so
// every seeded item was show_on_map=false (the Go zero value) with no
// coordinates, and the seeded trip's Map tab was empty until you edited each
// location by hand.
func seedFull(s seedCtx) error {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	start, end := day(today, 7), day(today, 10)

	trip, err := s.newTrip("full", "Iceland Ring Road", &start, &end,
		"A short demo trip seeded for local testing — nothing here is real.")
	if err != nil {
		return err
	}

	items, err := s.addItems("full", trip.ID, []itemSpec{
		// The link and date are here so the Links and Dates cards render with
		// content on the location view and editor pages. Without them both cards
		// only ever showed their empty state, so the UI sweeps never measured a
		// link-list row - which is how a 22px tap target in it survived until
		// Stage 09 Milestone 6 (found only because leftover manual test data
		// happened to be present that run).
		{key: "kirkjufell", category: "site", itemType: "landmark", title: "Kirkjufell",
			notes: "Iconic mountain on the Snæfellsnes peninsula.\n\n## Getting there\n\nPark at the waterfall lot.",
			lat:   ptr(64.9275), lng: ptr(-23.3106), onMap: true,
			links: []linkSpec{{url: "https://www.openstreetmap.org/?mlat=64.9275&mlon=-23.3106", label: "On the map"}},
			dates: []dateSpec{{start: "2026-08-20", end: "2026-08-20", label: "Sunrise shoot"}}},
		{key: "foss-hotel", category: "stay", itemType: "hotel", title: "Foss Hotel Reykjavik",
			notes: "Check-in from 15:00.", lat: ptr(64.1466), lng: ptr(-21.9426), onMap: true},
		{key: "kef-flight", category: "transport", itemType: "flight", title: "Flight to Keflavik",
			notes: "Seat 14A.", lat: ptr(63.9850), lng: ptr(-22.6056), onMap: true},
	})
	if err != nil {
		return err
	}

	// A cover photo on the trip and an image on one location, so the elements
	// that only ever rendered their empty state in the UI sweeps have something
	// to measure: the trip card's thumbnail on the trips list, the cover banner
	// on the trip page, .image-field__preview in Settings and the location
	// editor, .itinerary-entry__thumb on the itinerary (Kirkjufell is on day 2),
	// and the location card's thumbnail. Deliberately only *this* scenario, so
	// the no-image path stays covered too — the trips list shows one card with a
	// thumbnail and six without.
	coverID, err := s.addImage("full", trip.ID, "iceland-godafoss.jpg")
	if err != nil {
		return err
	}
	if _, err := s.store.SetTripPreviewImage(s.ctx, trip.ID, s.ownerID, &coverID, time.Now().UTC()); err != nil {
		return fmt.Errorf("set trip cover photo: %w", err)
	}
	itemImageID, err := s.addImage("full", trip.ID, "banff-moraine-lake.jpg")
	if err != nil {
		return err
	}
	if _, err := s.store.SetItemImage(s.ctx, items[0].ID, trip.ID, &itemImageID, time.Now().UTC()); err != nil {
		return fmt.Errorf("set location image: %w", err)
	}

	firstDay, err := s.addDay("full", trip.ID, start, ptr("Arrival day — take it easy."))
	if err != nil {
		return err
	}
	if err := s.addEntry("full", firstDay.ID, items[2].ID, 0); err != nil {
		return err
	}
	if err := s.addEntry("full", firstDay.ID, items[1].ID, 1); err != nil {
		return err
	}

	secondDay, err := s.addDay("full", trip.ID, day(today, 8), nil)
	if err != nil {
		return err
	}
	if err := s.addEntry("full", secondDay.ID, items[0].ID, 0); err != nil {
		return err
	}

	if err := s.addChecklist("full", trip.ID, "Packing", []string{
		"Passport", "Waterproof jacket", "Swimsuit", "Adapter"}); err != nil {
		return err
	}
	if err := s.addDocument("full", trip.ID, nil, "trip-notes.txt", "Seeded trip-level document.\n"); err != nil {
		return err
	}
	// Attached to a location, not the trip — the case the trip-level Documents
	// tab currently filters out (see todo.md).
	return s.addDocument("full", trip.ID, &items[1].ID, "hotel-booking.txt", "Seeded item-level document.\n")
}

// seedOnePin has a single mappable location, so the map's bounds are a point
// rather than a box — the degenerate-zoom case.
func seedOnePin(s seedCtx) error {
	start, end := day(baseDate, 0), day(baseDate, 2)
	trip, err := s.newTrip("one-pin", "Single Pin", &start, &end,
		"Exactly one mappable location: zero-size map bounds.")
	if err != nil {
		return err
	}
	_, err = s.addItems("one-pin", trip.ID, []itemSpec{
		{key: "only", category: "site", itemType: "museum", title: "The Only Pin",
			notes: "The single location on this trip's map.",
			lat:   ptr(52.2799), lng: ptr(8.0472), onMap: true},
		// Present but deliberately off the map, so "one pin" means one pin even
		// though the trip has two locations.
		{key: "hidden", category: "stay", itemType: "hostel", title: "Not On The Map",
			notes: "Has coordinates but show_on_map is false.",
			lat:   ptr(52.2700), lng: ptr(8.0400), onMap: false},
	})
	return err
}

func seedStartOnly(s seedCtx) error {
	start := day(baseDate, 30)
	_, err := s.newTrip("start-only", "Start Date Only", &start, nil,
		"Has a start date but no end date.")
	return err
}

// seedYearBoundary crosses 31 December, which is where naive date maths and
// "same year?" formatting decisions tend to break.
func seedYearBoundary(s seedCtx) error {
	yearEnd := time.Date(baseDate.Year(), time.December, 29, 0, 0, 0, 0, time.UTC)
	start, end := day(yearEnd, 0), day(yearEnd, 5)
	trip, err := s.newTrip("year-boundary", "New Year Crossing", &start, &end,
		"Runs from December into January.")
	if err != nil {
		return err
	}
	items, err := s.addItems("year-boundary", trip.ID, []itemSpec{
		{key: "party", category: "site", itemType: "event", title: "Midnight Fireworks",
			notes: "On the bridge.", lat: ptr(52.3676), lng: ptr(4.9041), onMap: true},
	})
	if err != nil {
		return err
	}
	// A day on each side of the boundary.
	for i, date := range []string{day(yearEnd, 2), day(yearEnd, 3)} {
		d, err := s.addDay("year-boundary", trip.ID, date, nil)
		if err != nil {
			return err
		}
		if err := s.addEntry("year-boundary", d.ID, items[0].ID, i); err != nil {
			return err
		}
	}
	return nil
}

// seedNoDates is the state the itinerary tab's empty copy talks about, and the
// one where trip-length maths has nothing to work with.
func seedNoDates(s seedCtx) error {
	trip, err := s.newTrip("no-dates", "No Dates Yet", nil, nil,
		"Neither a start nor an end date — the itinerary tab's empty state.")
	if err != nil {
		return err
	}
	_, err = s.addItems("no-dates", trip.ID, []itemSpec{
		{key: "idea", category: "site", itemType: "idea", title: "Somewhere, someday",
			notes: "An idea with no date attached yet."},
	})
	return err
}

// seedOutOfRangeDays puts itinerary days before and after the trip's own range,
// which is legal in the schema and has to render sensibly.
func seedOutOfRangeDays(s seedCtx) error {
	start, end := day(baseDate, 60), day(baseDate, 62)
	trip, err := s.newTrip("out-of-range-days", "Days Outside The Range", &start, &end,
		"Itinerary days both before and after the trip's own dates.")
	if err != nil {
		return err
	}
	items, err := s.addItems("out-of-range-days", trip.ID, []itemSpec{
		{key: "early", category: "transport", itemType: "train", title: "Early Arrival Train",
			notes: "Scheduled before the trip officially starts."},
	})
	if err != nil {
		return err
	}
	for i, offset := range []int{57, 61, 66} { // before, inside, after
		d, err := s.addDay("out-of-range-days", trip.ID, day(baseDate, offset), nil)
		if err != nil {
			return err
		}
		if err := s.addEntry("out-of-range-days", d.ID, items[0].ID, i); err != nil {
			return err
		}
	}
	return nil
}

// seedCascade exists to be deleted: it has a child of every kind that a trip
// delete has to clean up.
func seedCascade(s seedCtx) error {
	start, end := day(baseDate, 90), day(baseDate, 92)
	trip, err := s.newTrip("cascade", "Delete Me (Cascade)", &start, &end,
		"Has a child of every kind — delete this to check cascade behaviour.")
	if err != nil {
		return err
	}
	items, err := s.addItems("cascade", trip.ID, []itemSpec{
		{key: "a", category: "site", itemType: "landmark", title: "Cascade Location A",
			notes: "Has a location row, a document and an itinerary entry.",
			lat:   ptr(48.8584), lng: ptr(2.2945), onMap: true},
		{key: "b", category: "stay", itemType: "hotel", title: "Cascade Location B",
			notes: "Has an itinerary entry."},
	})
	if err != nil {
		return err
	}
	d, err := s.addDay("cascade", trip.ID, start, ptr("Day with entries to cascade."))
	if err != nil {
		return err
	}
	for i, item := range items {
		if err := s.addEntry("cascade", d.ID, item.ID, i); err != nil {
			return err
		}
	}
	if err := s.addChecklist("cascade", trip.ID, "Cascade Checklist", []string{"First", "Second"}); err != nil {
		return err
	}
	if err := s.addDocument("cascade", trip.ID, nil, "cascade-trip.txt", "Trip-level, should cascade.\n"); err != nil {
		return err
	}
	return s.addDocument("cascade", trip.ID, &items[0].ID, "cascade-item.txt", "Item-level, should cascade.\n")
}
