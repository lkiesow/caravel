package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound is returned by Store lookups that find no matching row,
// regardless of dialect (wraps sql.ErrNoRows so errors.Is(err, ErrNotFound) works).
var ErrNotFound = errors.New("not found")

type CreateUserParams struct {
	ID          string
	Username    string
	DisplayName string
	Email       *string
	IsAdmin     bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type UpdateUserParams struct {
	ID          string
	DisplayName string
	IsAdmin     bool
	UpdatedAt   time.Time
}

type CreateAuthIdentityParams struct {
	ID             string
	UserID         string
	Provider       string
	ProviderUserID string
	PasswordHash   *string
	CreatedAt      time.Time
}

type CreateSessionParams struct {
	ID         string
	UserID     string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
	UserAgent  *string
	IP         *string
}

type CreateTripParams struct {
	ID        string
	OwnerID   string
	Title     string
	StartDate *string
	EndDate   *string
	Subtitle  *string
	Currency  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// UpdateTripParams has no OwnerID: an editor may update a trip they do not
// own, so the query no longer filters by owner and authorization is entirely
// the caller's (see httpapi.Server.tripRole).
type UpdateTripParams struct {
	ID        string
	Title     string
	StartDate *string
	EndDate   *string
	Subtitle  *string
	Currency  string
	UpdatedAt time.Time
}

type CreateFileParams struct {
	ID          string
	TripID      string
	ItemID      *string
	Filename    string
	StoragePath string
	ContentType *string
	Visibility  FileVisibility
	OwnerUserID *string
	SizeBytes   int64
	UploadedAt  time.Time
	Note        *string
}

type CreateExpenseParams struct {
	ID          string
	TripID      string
	Title       string
	AmountMinor int64
	SpentOn     string
	PayerUserID *string
	CreatedAt   time.Time
}

// UpdateExpenseParams keeps TripID where UpdateTripParams dropped its OwnerID:
// here it is not an authorization shortcut but the thing that stops an expense
// id belonging to one trip from being edited through a role held on another.
type UpdateExpenseParams struct {
	ID          string
	TripID      string
	Title       string
	AmountMinor int64
	SpentOn     string
	PayerUserID *string
}

type CreateChecklistParams struct {
	ID          string
	TripID      string
	Title       string
	SortOrder   int
	CreatedAt   time.Time
	Visibility  ChecklistVisibility
	OwnerUserID *string
}

type CreateChecklistItemParams struct {
	ID          string
	ChecklistID string
	Text        string
	Checked     bool
	SortOrder   int
	CreatedAt   time.Time
}

type CreateItineraryEntryParams struct {
	ID             string
	ItineraryDayID string
	ItemID         string
	SortOrder      int
	Note           *string
}

type CreateMediaAssetParams struct {
	ID          string
	TripID      string
	Kind        string // "upload" | "url"
	StoragePath *string
	ExternalURL *string
	ContentType *string
	Width       *int
	Height      *int
	SourceURL   *string
	Credit      *string
	License     *string
	CreatedAt   time.Time
}

type CreateItemParams struct {
	ID        string
	TripID    string
	Category  string
	Type      string
	Title     string
	Notes     *string
	ShowOnMap bool
	SortOrder int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type UpdateItemParams struct {
	ID        string
	TripID    string
	Category  string
	Type      string
	Title     string
	Notes     *string
	ShowOnMap bool
	SortOrder int
	UpdatedAt time.Time
}

type UpsertItemLocationParams struct {
	ID      string
	ItemID  string
	Lat     *float64
	Lng     *float64
	Address *string
}

type CreateItemLinkParams struct {
	ID        string
	ItemID    string
	URL       string
	Label     *string
	SortOrder int
}

type CreateItemDateParams struct {
	ID        string
	ItemID    string
	StartDate *string
	EndDate   *string
	Label     *string
	AllDay    bool
	StartTime *string
	EndTime   *string
}

// Store is the dialect-agnostic data access interface. internal/db provides
// one implementation per supported database (sqliteStore, postgresStore),
// each wrapping its own sqlc-generated Queries and converting rows into the
// shared domain types in domain.go.
type Store interface {
	CreateUser(ctx context.Context, p CreateUserParams) (User, error)
	GetUserByID(ctx context.Context, id string) (User, error)
	GetUserByUsername(ctx context.Context, username string) (User, error)
	// SearchUsers finds users whose username or display name contains `query`,
	// case-insensitively, capped at limit. Matching is done on a lowercased
	// pattern built here, so callers pass the raw text.
	SearchUsers(ctx context.Context, query string, limit int) ([]UserSummary, error)
	// CountUsers answers one question, asked in two places: is this the first
	// account on the instance? That decides both whether it becomes an admin
	// and whether a closed registration accepts it anyway.
	CountUsers(ctx context.Context) (int64, error)
	// ListUsers is the admin screen's list, ordered by username.
	ListUsers(ctx context.Context) ([]UserWithTripCount, error)
	// CountAdmins backs the last-admin guard rails: an instance must never end
	// up with nobody who can administer it.
	CountAdmins(ctx context.Context) (int64, error)
	// UpdateUser changes a user's display name and admin flag. Username is not
	// updatable on purpose — see the query.
	UpdateUser(ctx context.Context, p UpdateUserParams) (User, error)
	// DeleteUser reports whether a user was actually removed. Their trips,
	// sessions, identities and memberships go with them by cascade.
	DeleteUser(ctx context.Context, id string) (bool, error)

	// Instance settings. GetAppSetting returns ErrNotFound for an unset name,
	// so callers decide their own default rather than being handed a zero
	// value that might be a legitimate setting.
	GetAppSetting(ctx context.Context, name string) (string, error)
	SetAppSetting(ctx context.Context, name, value string) error

	CreateAuthIdentity(ctx context.Context, p CreateAuthIdentityParams) (AuthIdentity, error)
	GetAuthIdentityByProvider(ctx context.Context, provider, providerUserID string) (AuthIdentity, error)
	// UpdateAuthIdentityPassword replaces the stored hash for one identity.
	UpdateAuthIdentityPassword(ctx context.Context, provider, providerUserID, passwordHash string) error

	CreateSession(ctx context.Context, p CreateSessionParams) (Session, error)
	GetSessionByID(ctx context.Context, id string) (Session, error)
	TouchSession(ctx context.Context, id string, lastSeenAt, expiresAt time.Time) error
	DeleteSession(ctx context.Context, id string) error
	// DeleteSessionsByUserID logs a user out everywhere, used when their
	// password changes.
	DeleteSessionsByUserID(ctx context.Context, userID string) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) error

	CreateTrip(ctx context.Context, p CreateTripParams) (Trip, error)
	GetTripByID(ctx context.Context, id string) (Trip, error)
	// ListTripsForUser returns every trip the user owns or is a member of,
	// each carrying their role on it and the owner's name.
	ListTripsForUser(ctx context.Context, userID string) ([]TripForUser, error)
	UpdateTrip(ctx context.Context, p UpdateTripParams) (Trip, error)
	// DeleteTrip reports whether a matching (id, ownerID) trip was deleted.
	DeleteTrip(ctx context.Context, id, ownerID string) (bool, error)
	// SetTripPreviewImage sets (or clears, if imageID is nil) a trip's preview
	// image. Not owner-scoped — an editor may change a trip's cover photo.
	SetTripPreviewImage(ctx context.Context, id string, imageID *string, updatedAt time.Time) (Trip, error)

	// Trip membership. The owner is not stored as a member (see migration
	// 0007), so every one of these answers a question about non-owners only.
	// GetTripMember returns ErrNotFound when the user is not a member —
	// including when they are the owner.
	GetTripMember(ctx context.Context, tripID, userID string) (TripMember, error)
	ListTripMembers(ctx context.Context, tripID string) ([]TripMember, error)
	// UpsertTripMember adds a member or changes an existing member's role;
	// role must be Assignable (editor or viewer).
	UpsertTripMember(ctx context.Context, tripID, userID string, role TripRole, createdAt time.Time) (TripMember, error)
	// DeleteTripMember reports whether a membership was actually removed.
	DeleteTripMember(ctx context.Context, tripID, userID string) (bool, error)
	CountTripMembers(ctx context.Context, tripID string) (int64, error)

	CreateItem(ctx context.Context, p CreateItemParams) (Item, error)
	GetItemByID(ctx context.Context, id string) (Item, error)
	// ListItemsByTrip lists items for a trip, optionally filtered by category.
	ListItemsByTrip(ctx context.Context, tripID string, category *string) ([]Item, error)
	UpdateItem(ctx context.Context, p UpdateItemParams) (Item, error)
	DeleteItem(ctx context.Context, id, tripID string) (bool, error)
	// SetItemImage sets (or clears, if imageID is nil) an item's image.
	SetItemImage(ctx context.Context, id, tripID string, imageID *string, updatedAt time.Time) (Item, error)

	UpsertItemLocation(ctx context.Context, p UpsertItemLocationParams) (ItemLocation, error)
	GetItemLocationByItemID(ctx context.Context, itemID string) (ItemLocation, error)

	CreateItemLink(ctx context.Context, p CreateItemLinkParams) (ItemLink, error)
	ListItemLinksByItem(ctx context.Context, itemID string) ([]ItemLink, error)
	DeleteItemLink(ctx context.Context, id, itemID string) (bool, error)

	CreateItemDate(ctx context.Context, p CreateItemDateParams) (ItemDate, error)
	ListItemDatesByItem(ctx context.Context, itemID string) ([]ItemDate, error)
	DeleteItemDate(ctx context.Context, id, itemID string) (bool, error)

	CreateMediaAsset(ctx context.Context, p CreateMediaAssetParams) (MediaAsset, error)
	GetMediaAssetByID(ctx context.Context, id string) (MediaAsset, error)

	// ListMapItems returns items with a resolvable location and show_on_map=true.
	ListMapItems(ctx context.Context, tripID string) ([]MapItem, error)

	// ListItemCoordinates returns every located item on the trip, regardless
	// of show_on_map — see ItemCoordinate for why that differs from
	// ListMapItems.
	ListItemCoordinates(ctx context.Context, tripID string) ([]ItemCoordinate, error)

	// UpsertItineraryDayNotes creates the day row if needed (this is the only
	// way itinerary_days rows come into existence — see plan Section 5) and
	// sets its notes. newID is used only if a new row must be inserted.
	UpsertItineraryDayNotes(ctx context.Context, newID, tripID, date string, notes *string) (ItineraryDay, error)
	// EnsureItineraryDay returns the day for (tripID, date), creating it with
	// no notes if it does not exist yet. It differs from
	// UpsertItineraryDayNotes in the one way that matters to a caller who only
	// wants the row to exist: passing nil notes to the upsert *clears* the
	// notes of a day that already has them, which is right for "set the notes
	// to nothing" and wrong for "make sure this day is there". newID is used
	// only if a row must be inserted.
	//
	// Two callers racing to create the same day is a unique-constraint
	// violation on (trip_id, date) rather than a silent second row, and the
	// error is returned as-is: inside a transaction there is nothing useful to
	// do with it locally, so the caller rolls back and the client retries.
	EnsureItineraryDay(ctx context.Context, newID, tripID, date string) (ItineraryDay, error)
	ListItineraryDaysByTrip(ctx context.Context, tripID string) ([]ItineraryDay, error)
	GetItineraryDayByID(ctx context.Context, id string) (ItineraryDay, error)
	// DeleteItineraryDay removes the day and, through the entries table's
	// ON DELETE CASCADE, everything planned on it. Reports false if no row
	// matched both id and tripID.
	DeleteItineraryDay(ctx context.Context, id, tripID string) (bool, error)

	CreateItineraryEntry(ctx context.Context, p CreateItineraryEntryParams) (ItineraryEntry, error)
	ListItineraryEntriesByTrip(ctx context.Context, tripID string) ([]ItineraryEntryDetail, error)
	// ListItineraryEntriesByDay returns one day's entries in stored order. Used
	// to number a new entry, and to check a reorder against the entries the day
	// actually has.
	ListItineraryEntriesByDay(ctx context.Context, itineraryDayID string) ([]ItineraryEntry, error)
	// SetItineraryEntrySortOrder reports whether it matched a row, so a reorder
	// naming an entry from another day fails rather than silently doing nothing.
	SetItineraryEntrySortOrder(ctx context.Context, id, itineraryDayID string, sortOrder int) (bool, error)
	// SetItineraryEntryDay moves an entry to another day, giving it a position
	// there at the same time. fromDayID is the day the entry is expected to be
	// on: reporting false rather than moving something unexpected is what makes
	// a move safe to run against a client that read the itinerary a moment ago.
	SetItineraryEntryDay(ctx context.Context, id, fromDayID, toDayID string, sortOrder int) (bool, error)
	DeleteItineraryEntry(ctx context.Context, id, itineraryDayID string) (bool, error)

	CreateFile(ctx context.Context, p CreateFileParams) (File, error)
	GetFileByID(ctx context.Context, id string) (File, error)
	// ListTripFiles returns every file on the trip — both trip-level
	// ones and those attached to one of its locations — newest first, each
	// carrying the title of the location it belongs to (nil for trip-level).
	// It used to filter item_id IS NULL, which hid location-attached files
	// from the trip's Files tab even though they are files on that trip.
	// ListTripFiles and ListItemFiles both take the *reading* user, because a
	// personal file belongs to whoever uploaded it and must not appear in
	// anyone else's list.
	ListTripFiles(ctx context.Context, tripID, userID string) ([]FileDetail, error)
	ListItemFiles(ctx context.Context, itemID, userID string) ([]File, error)
	// UpdateFileNote replaces a file's note, or clears it when note is nil —
	// the one field a file has that can change after upload. Scoped by trip
	// like DeleteFile; returns ErrNotFound if no row matches both.
	UpdateFileNote(ctx context.Context, id, tripID string, note *string) (File, error)
	// SetFileVisibility is separate from UpdateFileNote because the two carry
	// different authorization rules: an editor may retitle shared content, but
	// only the uploader may make their own file personal or public.
	SetFileVisibility(ctx context.Context, id, tripID string, visibility FileVisibility) (File, error)
	// ListPersonalFilesForUser finds the rows to remove when someone stops
	// being a member of a trip. Returned rather than deleted in one statement
	// so the caller can delete each blob too.
	ListPersonalFilesForUser(ctx context.Context, tripID, userID string) ([]File, error)
	DeleteFile(ctx context.Context, id, tripID string) (bool, error)

	CreateChecklist(ctx context.Context, p CreateChecklistParams) (Checklist, error)
	GetChecklistByID(ctx context.Context, id string) (Checklist, error)
	// ListChecklistsByTrip takes the reading user: a personal list belongs to
	// whoever created it and must not appear in anyone else's list.
	ListChecklistsByTrip(ctx context.Context, tripID, userID string) ([]Checklist, error)
	// SetChecklistVisibility is author-only; UpdateChecklistTitle follows the
	// list's own write rule (see httpapi.canModifyChecklist).
	SetChecklistVisibility(ctx context.Context, id, tripID string, visibility ChecklistVisibility) (Checklist, error)
	UpdateChecklistTitle(ctx context.Context, id, tripID, title string) (Checklist, error)
	// ListPersonalChecklistsForUser finds what to remove when someone stops
	// being a member of a trip.
	ListPersonalChecklistsForUser(ctx context.Context, tripID, userID string) ([]Checklist, error)
	DeleteChecklist(ctx context.Context, id, tripID string) (bool, error)

	CreateChecklistItem(ctx context.Context, p CreateChecklistItemParams) (ChecklistItem, error)
	ListChecklistItemsByChecklist(ctx context.Context, checklistID string) ([]ChecklistItem, error)
	SetChecklistItemChecked(ctx context.Context, id, checklistID string, checked bool) (ChecklistItem, error)
	UpdateChecklistItemText(ctx context.Context, id, checklistID, text string) (ChecklistItem, error)
	DeleteChecklistItem(ctx context.Context, id, checklistID string) (bool, error)

	// Expenses. No reading user is threaded through any of these, unlike the
	// file and checklist listings: every expense on a trip is visible to
	// everyone on it, so there is nothing per-reader to filter.
	CreateExpense(ctx context.Context, p CreateExpenseParams) (Expense, error)
	GetExpenseByID(ctx context.Context, id string) (Expense, error)
	ListExpensesByTrip(ctx context.Context, tripID string) ([]Expense, error)
	// SumExpensesByTrip totals the trip in minor units, answered by the
	// database rather than by summing what a caller happens to be listing.
	SumExpensesByTrip(ctx context.Context, tripID string) (int64, error)
	UpdateExpense(ctx context.Context, p UpdateExpenseParams) (Expense, error)
	// DeleteExpense reports whether a matching (id, tripID) expense was deleted.
	DeleteExpense(ctx context.Context, id, tripID string) (bool, error)

	// Expense shares: who an expense was for. An expense with no shares is
	// for everyone on the trip, resolved by the caller rather than stored --
	// see migration 0012.
	//
	// The set is replaced wholesale rather than patched, so callers run
	// DeleteExpenseSharesByExpense and then CreateExpenseShare inside one
	// WithTx.
	CreateExpenseShare(ctx context.Context, expenseID, userID string) error
	DeleteExpenseSharesByExpense(ctx context.Context, expenseID string) error
	// ListExpenseShareUsers returns the user ids sharing one expense, sorted.
	ListExpenseShareUsers(ctx context.Context, expenseID string) ([]string, error)
	// ListExpenseSharesByTrip returns every share on a trip, so a listing
	// costs one query rather than one per expense.
	ListExpenseSharesByTrip(ctx context.Context, tripID string) ([]ExpenseShare, error)

	// WithTx runs fn with a Store bound to a single transaction, committing
	// on success and rolling back if fn returns an error.
	WithTx(ctx context.Context, fn func(Store) error) error
}

// likeContains builds the LIKE pattern for a case-insensitive "contains"
// search. Lowercased to match the LOWER() on the column side.
//
// LIKE's own wildcards are *not* escaped: see the note on SearchUsers in
// queries/users.sql — sqlc's sqlite grammar rejects the ESCAPE clause that
// would make escaping mean the same thing in both dialects, and escaping
// without it would work in one and silently not in the other. So a typed % or
// _ behaves as a wildcard here, which for a search box is a feature at worst.
func likeContains(query string) string {
	return "%" + strings.ToLower(query) + "%"
}

// NewStore builds the Store implementation for the given driver, wrapping an
// already-open, already-migrated *sql.DB (see db.Open).
func NewStore(driver string, conn *sql.DB) (Store, error) {
	switch driver {
	case "sqlite":
		return newSQLiteStore(conn), nil
	case "postgres":
		return newPostgresStore(conn), nil
	default:
		return nil, fmt.Errorf("unknown db driver %q", driver)
	}
}

func strPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

func nullString(p *string) sql.NullString {
	if p == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *p, Valid: true}
}

func floatPtr(nf sql.NullFloat64) *float64 {
	if !nf.Valid {
		return nil
	}
	v := nf.Float64
	return &v
}

func nullFloat64(p *float64) sql.NullFloat64 {
	if p == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *p, Valid: true}
}

func intPtr64(ni sql.NullInt64) *int {
	if !ni.Valid {
		return nil
	}
	v := int(ni.Int64)
	return &v
}

func nullInt64(p *int) sql.NullInt64 {
	if p == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*p), Valid: true}
}

func intPtr32(ni sql.NullInt32) *int {
	if !ni.Valid {
		return nil
	}
	v := int(ni.Int32)
	return &v
}

func nullInt32(p *int) sql.NullInt32 {
	if p == nil {
		return sql.NullInt32{}
	}
	return sql.NullInt32{Int32: int32(*p), Valid: true}
}
