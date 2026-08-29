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

	// A third account whose only purpose is to be broken and repaired by
	// tests/ui/settings.spec.js, which changes a password and restores it.
	// Changing a password deletes every session that account holds, so the
	// account doing that cannot be one another spec needs a live session for —
	// which is exactly the collision that made sharing.spec.js fail in a full
	// run while passing alone: it holds a saved session for `other`, and the
	// password spec was killing it mid-run.
	pwUsername    = "pwtest"
	pwPassword    = "pwtest1234"
	pwDisplayName = "Password Test"

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
	{"full", "everything populated: locations with coordinates on the map, itinerary, checklist, files", seedFull},
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

	// demo is the administrator, so the admin screen has somebody to be reached
	// by; other deliberately is not, so the "a non-admin sees no admin entry"
	// case has somebody to be checked with.
	owner, err := ensureUser(ctx, store, authService, demoUsername, demoPassword, demoDisplayName, true)
	if err != nil {
		log.Fatalf("demo user: %v", err)
	}
	other, err := ensureUser(ctx, store, authService, otherUsername, otherPassword, otherDisplayName, false)
	if err != nil {
		log.Fatalf("other user: %v", err)
	}
	// Not given any trip or membership: it exists to have its password churned,
	// and giving it content would make it a second `other` by accident.
	if _, err := ensureUser(ctx, store, authService, pwUsername, pwPassword, pwDisplayName, false); err != nil {
		log.Fatalf("password-test user: %v", err)
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

// ensureUser makes the documented dev credentials true, whether or not the user
// already exists.
//
// The password reset on the existing-user path is not redundant: since Stage 12
// a password can be changed from the settings screen (and the UI suite changes
// the "other" account's on purpose), so a user whose password had drifted could
// not be recovered by re-seeding - the seeder printed credentials that no longer
// worked and the only fix was wiping the database. Resetting is idempotent and
// leaves sessions alone, so re-seeding doesn't log you out of the browser you
// are testing in.
func ensureUser(ctx context.Context, store db.Store, authService *auth.Service, username, password, displayName string, admin bool) (db.User, error) {
	user, err := authService.Register(ctx, username, password, displayName)
	if err != nil {
		if err != auth.ErrUsernameTaken {
			return db.User{}, err
		}
		if err := authService.SetPassword(ctx, username, password); err != nil {
			return db.User{}, fmt.Errorf("reset password for %q: %w", username, err)
		}
		log.Printf("user %q already exists — password reset to %s", username, password)
		if user, err = store.GetUserByUsername(ctx, username); err != nil {
			return db.User{}, err
		}
	} else {
		log.Printf("created user %q (password: %s)", username, password)
	}

	// The admin flag is set explicitly rather than relied on. Register makes
	// the *first* account on an instance an admin, which happens to give demo
	// the flag on a fresh database — but not on a database seeded before
	// migration 0008 existed, where the column simply defaulted to false. A
	// dev environment whose admin-ness depends on how old its database is would
	// be a confusing thing to debug.
	if user.IsAdmin != admin {
		if user, err = store.UpdateUser(ctx, db.UpdateUserParams{
			ID:          user.ID,
			DisplayName: user.DisplayName,
			IsAdmin:     admin,
			UpdatedAt:   time.Now().UTC(),
		}); err != nil {
			return db.User{}, fmt.Errorf("set admin flag for %q: %w", username, err)
		}
	}
	if admin {
		log.Printf("user %q is an administrator", username)
	}
	return user, nil
}

// newTrip deletes any previous incarnation of this scenario's trip before
// creating it, which is what makes re-running the seed idempotent: the trip's ID
// is deterministic, so without the delete a second run would collide on the
// primary key. DeleteTrip cascades to items, days, entries, checklists and
// files, so this clears the whole scenario.
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
		Currency:  db.DefaultCurrency,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

type linkSpec struct {
	url   string
	label string
}

type itemSpec struct {
	key      string // stable per-trip identity, for the deterministic ID
	category string
	// tags is what itemType was until Stage 26 Milestone 7 folded the type
	// field into the tag set. Seeded data carries real tags now, which the UI
	// suite needs: the tag filter, the chips on the card and the remove button
	// in the editor all had nothing to look at while every seeded location was
	// untagged.
	tags     []string
	title    string
	notes    string
	lat, lng *float64 // nil = no coordinates at all
	onMap    bool
	links    []linkSpec
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
		for _, tag := range spec.tags {
			if err := s.store.CreateItemTag(s.ctx, itemID, tag); err != nil {
				return nil, fmt.Errorf("tag item %s: %w", spec.key, err)
			}
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

// Visibility is a required parameter rather than a defaulted one: the sweeps can
// only see a visibility that some scenario actually seeds, so "which one?" is a
// question every caller should have to answer out loud.
func (s seedCtx) addChecklist(scenarioName, tripID, title string, vis db.ChecklistVisibility, items []string) error {
	// Visibility and owner are explicit, not left to the column default: the
	// store passes whatever the params carry, so an unset visibility inserts the
	// empty string and trips the CHECK constraint. Which is how this was found —
	// migration 0010 landed and the next `make dev-reset` failed on the first
	// scenario.
	list, err := s.store.CreateChecklist(s.ctx, db.CreateChecklistParams{
		ID:          seedID(scenarioName, "checklist", title),
		TripID:      tripID,
		Title:       title,
		SortOrder:   0,
		CreatedAt:   time.Now().UTC(),
		Visibility:  vis,
		OwnerUserID: &s.ownerID,
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

// addFile writes a real (tiny) file through the blob store as well as the
// row, so the Files tab has something that actually downloads.
// Visibility is a required parameter for the same reason as addChecklist above.
func (s seedCtx) addFile(scenarioName, tripID string, itemID *string, filename, body string, vis db.FileVisibility) error {
	id := seedID(scenarioName, "file", filename)
	key := fmt.Sprintf("%s/%s", tripID, id)
	size, err := s.blob.Put(s.ctx, key, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("store blob for %s: %w", filename, err)
	}
	_, err = s.store.CreateFile(s.ctx, db.CreateFileParams{
		ID:          id,
		TripID:      tripID,
		ItemID:      itemID,
		Filename:    filename,
		StoragePath: key,
		ContentType: ptr("text/plain; charset=utf-8"),
		SizeBytes:   size,
		UploadedAt:  time.Now().UTC(),
		Note:        ptr("Seeded file"),
		// Same as the checklist above: explicit, because the params win over the
		// column default.
		Visibility:  vis,
		OwnerUserID: &s.ownerID,
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
		// The link is here so the Links card renders with content on the
		// location view and editor pages. Without it the card only ever showed
		// its empty state, so the UI sweeps never measured a link-list row -
		// which is how a 22px tap target in it survived until Stage 09
		// Milestone 6 (found only because leftover manual test data happened to
		// be present that run).
		//
		// The Dates card needs nothing here since Stage 25: a location dates
		// are the itinerary days it is on, and this one is put on a day below.
		// That is the unification working -- one fact, seeded once.
		{key: "kirkjufell", category: "site", tags: []string{"landmark"}, title: "Kirkjufell",
			notes: "Iconic mountain on the Snæfellsnes peninsula.\n\n## Getting there\n\nPark at the waterfall lot.",
			lat:   ptr(64.9275), lng: ptr(-23.3106), onMap: true,
			links: []linkSpec{{url: "https://www.openstreetmap.org/?mlat=64.9275&mlon=-23.3106", label: "On the map"}}},
		{key: "foss-hotel", category: "stay", tags: []string{"hotel"}, title: "Foss Hotel Reykjavik",
			notes: "Check-in from 15:00.", lat: ptr(64.1466), lng: ptr(-21.9426), onMap: true},
		{key: "kef-flight", category: "transport", tags: []string{"flight"}, title: "Flight to Keflavik",
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
	if _, err := s.store.SetTripPreviewImage(s.ctx, trip.ID, &coverID, time.Now().UTC()); err != nil {
		return fmt.Errorf("set trip cover photo: %w", err)
	}
	// `other` is an editor here and a viewer on one-pin, so both halves of the
	// role model have a scenario: the sweeps see a trip with members, and the
	// sharing spec has an editor case and a read-only case without creating
	// either itself.
	if err := s.addMember(trip.ID, db.RoleEditor); err != nil {
		return err
	}

	// A ledger with one of each interesting shape: split with the whole trip,
	// paid by the other person rather than the owner, shared with a subset (so
	// the row carries its "Only for ..." line and the balance does not follow
	// from the amounts alone), and one nobody is recorded as paying, which the
	// balances report rather than absorb.
	if err := s.addExpenses("full", trip.ID, []expenseSpec{
		{key: "hotel", title: "Hótel Reykjavík, two nights", amount: 34000, dayOff: 0, payer: "owner"},
		{key: "fuel", title: "Fuel, Route 1", amount: 8750, dayOff: 1, payer: "other"},
		{key: "lagoon", title: "Blue Lagoon, one ticket", amount: 12750, dayOff: 1, payer: "owner",
			shares: []string{"other"}},
		{key: "parking", title: "Parking, unknown who paid", amount: 300, dayOff: 2},
	}); err != nil {
		return err
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

	// One checklist per visibility, and one file per visibility. This is not
	// padding: the route sweeps (overflow, tap targets, headings, accessible
	// names, contrast) can only measure markup that some scenario renders, so
	// until Stage 14 Milestone 10 seeded these, the personal-files section, its
	// lock badge and the two non-shared checklist states had never once been
	// drawn by any automated check.
	if err := s.addChecklist("full", trip.ID, "Packing", db.ChecklistShared, []string{
		"Passport", "Waterproof jacket", "Swimsuit", "Adapter"}); err != nil {
		return err
	}
	if err := s.addChecklist("full", trip.ID, "Route plan", db.ChecklistTrip, []string{
		"Book the ferry", "Print the permits"}); err != nil {
		return err
	}
	if err := s.addChecklist("full", trip.ID, "My own packing", db.ChecklistPersonal, []string{
		"Contact lenses", "Camera charger"}); err != nil {
		return err
	}
	if err := s.addFile("full", trip.ID, nil, "trip-notes.txt", "Seeded trip-level file.\n", db.FileVisibilityTrip); err != nil {
		return err
	}
	if err := s.addFile("full", trip.ID, nil, "my-insurance.txt", "Seeded personal file.\n", db.FileVisibilityPersonal); err != nil {
		return err
	}
	// Attached to a location, not the trip — the case the trip-level Files
	// tab currently filters out (see todo.md).
	return s.addFile("full", trip.ID, &items[1].ID, "hotel-booking.txt", "Seeded item-level file.\n", db.FileVisibilityTrip)
}

// seedOnePin has a single mappable location, so the map's bounds are a point
// rather than a box — the degenerate-zoom case.
// expenseSpec is one seeded expense. payer and shares name people by the same
// keys the scenarios use for readability -- "owner", "other", or "" for nobody.
type expenseSpec struct {
	key    string
	title  string
	amount int64
	dayOff int
	payer  string
	shares []string
}

// addExpenses seeds a trip ledger. Worth having for the same reason the
// checklists are seeded: the tab is hard to judge empty, and the interesting
// cases are the ones nobody creates by accident -- an expense shared with a
// subset of the trip, and one nobody is recorded as paying.
//
// Deterministic ids like everything else here, so re-seeding replaces the rows
// rather than piling up a second ledger.
func (s seedCtx) addExpenses(scenarioName, tripID string, specs []expenseSpec) error {
	now := time.Now().UTC()
	person := map[string]string{"owner": s.ownerID, "other": s.otherID}
	for _, spec := range specs {
		expenseID := seedID(scenarioName, "expense", spec.key)
		var payer *string
		if id, ok := person[spec.payer]; ok {
			payer = ptr(id)
		}
		if _, err := s.store.CreateExpense(s.ctx, db.CreateExpenseParams{
			ID:          expenseID,
			TripID:      tripID,
			Title:       spec.title,
			AmountMinor: spec.amount,
			SpentOn:     day(baseDate, spec.dayOff),
			PayerUserID: payer,
			CreatedAt:   now,
		}); err != nil {
			return fmt.Errorf("create expense %s: %w", spec.key, err)
		}
		// No share rows means everyone on the trip, so the common case writes
		// nothing at all -- see migration 0012.
		for _, who := range spec.shares {
			id, ok := person[who]
			if !ok {
				return fmt.Errorf("expense %s: unknown share %q", spec.key, who)
			}
			if err := s.store.CreateExpenseShare(s.ctx, expenseID, id); err != nil {
				return fmt.Errorf("share expense %s with %s: %w", spec.key, who, err)
			}
		}
	}
	return nil
}

// addMember puts `other` on a trip, so the UI suite has a real shared trip to
// drive rather than having to create one itself — and so the trips list shows
// both a shared card and owned ones side by side.
func (s seedCtx) addMember(tripID string, role db.TripRole) error {
	if _, err := s.store.UpsertTripMember(s.ctx, tripID, s.otherID, role, time.Now().UTC()); err != nil {
		return fmt.Errorf("add member (%s): %w", role, err)
	}
	return nil
}

func seedOnePin(s seedCtx) error {
	start, end := day(baseDate, 0), day(baseDate, 2)
	trip, err := s.newTrip("one-pin", "Single Pin", &start, &end,
		"Exactly one mappable location: zero-size map bounds.")
	if err != nil {
		return err
	}
	_, err = s.addItems("one-pin", trip.ID, []itemSpec{
		{key: "only", category: "site", tags: []string{"museum"}, title: "The Only Pin",
			notes: "The single location on this trip's map.",
			lat:   ptr(52.2799), lng: ptr(8.0472), onMap: true},
		// Present but deliberately off the map, so "one pin" means one pin even
		// though the trip has two locations.
		{key: "hidden", category: "stay", tags: []string{"hostel"}, title: "Not On The Map",
			notes: "Has coordinates but show_on_map is false.",
			lat:   ptr(52.2700), lng: ptr(8.0400), onMap: false},
	})
	if err != nil {
		return err
	}
	// The read-only case: `other` can see this trip but change nothing on it.
	return s.addMember(trip.ID, db.RoleViewer)
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
		{key: "party", category: "site", tags: []string{"event"}, title: "Midnight Fireworks",
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
		{key: "idea", category: "site", tags: []string{"idea"}, title: "Somewhere, someday",
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
		{key: "early", category: "transport", tags: []string{"train"}, title: "Early Arrival Train",
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
		{key: "a", category: "site", tags: []string{"landmark"}, title: "Cascade Location A",
			notes: "Has a location row, a file and an itinerary entry.",
			lat:   ptr(48.8584), lng: ptr(2.2945), onMap: true},
		{key: "b", category: "stay", tags: []string{"hotel"}, title: "Cascade Location B",
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
	if err := s.addChecklist("cascade", trip.ID, "Cascade Checklist", db.ChecklistShared, []string{"First", "Second"}); err != nil {
		return err
	}
	if err := s.addFile("cascade", trip.ID, nil, "cascade-trip.txt", "Trip-level, should cascade.\n", db.FileVisibilityTrip); err != nil {
		return err
	}
	return s.addFile("cascade", trip.ID, &items[0].ID, "cascade-item.txt", "Item-level, should cascade.\n", db.FileVisibilityTrip)
}
