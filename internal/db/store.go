package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	CreatedAt   time.Time
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
	CreatedAt time.Time
	UpdatedAt time.Time
}

type UpdateTripParams struct {
	ID        string
	OwnerID   string
	Title     string
	StartDate *string
	EndDate   *string
	Subtitle  *string
	UpdatedAt time.Time
}

type CreateFileParams struct {
	ID          string
	TripID      string
	ItemID      *string
	Filename    string
	StoragePath string
	ContentType *string
	SizeBytes   int64
	UploadedAt  time.Time
	Note        *string
}

type CreateChecklistParams struct {
	ID        string
	TripID    string
	Title     string
	SortOrder int
	CreatedAt time.Time
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

	CreateAuthIdentity(ctx context.Context, p CreateAuthIdentityParams) (AuthIdentity, error)
	GetAuthIdentityByProvider(ctx context.Context, provider, providerUserID string) (AuthIdentity, error)

	CreateSession(ctx context.Context, p CreateSessionParams) (Session, error)
	GetSessionByID(ctx context.Context, id string) (Session, error)
	TouchSession(ctx context.Context, id string, lastSeenAt, expiresAt time.Time) error
	DeleteSession(ctx context.Context, id string) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) error

	CreateTrip(ctx context.Context, p CreateTripParams) (Trip, error)
	GetTripByID(ctx context.Context, id string) (Trip, error)
	ListTripsByOwner(ctx context.Context, ownerID string) ([]Trip, error)
	UpdateTrip(ctx context.Context, p UpdateTripParams) (Trip, error)
	// DeleteTrip reports whether a matching (id, ownerID) trip was deleted.
	DeleteTrip(ctx context.Context, id, ownerID string) (bool, error)
	// SetTripPreviewImage sets (or clears, if imageID is nil) a trip's preview image.
	SetTripPreviewImage(ctx context.Context, id, ownerID string, imageID *string, updatedAt time.Time) (Trip, error)

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

	// UpsertItineraryDayNotes creates the day row if needed (this is the only
	// way itinerary_days rows come into existence — see plan Section 5) and
	// sets its notes. newID is used only if a new row must be inserted.
	UpsertItineraryDayNotes(ctx context.Context, newID, tripID, date string, notes *string) (ItineraryDay, error)
	ListItineraryDaysByTrip(ctx context.Context, tripID string) ([]ItineraryDay, error)
	GetItineraryDayByID(ctx context.Context, id string) (ItineraryDay, error)
	// DeleteItineraryDay removes the day and, through the entries table's
	// ON DELETE CASCADE, everything planned on it. Reports false if no row
	// matched both id and tripID.
	DeleteItineraryDay(ctx context.Context, id, tripID string) (bool, error)

	CreateItineraryEntry(ctx context.Context, p CreateItineraryEntryParams) (ItineraryEntry, error)
	ListItineraryEntriesByTrip(ctx context.Context, tripID string) ([]ItineraryEntryDetail, error)
	DeleteItineraryEntry(ctx context.Context, id, itineraryDayID string) (bool, error)

	CreateFile(ctx context.Context, p CreateFileParams) (File, error)
	GetFileByID(ctx context.Context, id string) (File, error)
	// ListTripFiles returns every file on the trip — both trip-level
	// ones and those attached to one of its locations — newest first, each
	// carrying the title of the location it belongs to (nil for trip-level).
	// It used to filter item_id IS NULL, which hid location-attached files
	// from the trip's Files tab even though they are files on that trip.
	ListTripFiles(ctx context.Context, tripID string) ([]FileDetail, error)
	ListItemFiles(ctx context.Context, itemID string) ([]File, error)
	// UpdateFileNote replaces a file's note, or clears it when note is nil —
	// the one field a file has that can change after upload. Scoped by trip
	// like DeleteFile; returns ErrNotFound if no row matches both.
	UpdateFileNote(ctx context.Context, id, tripID string, note *string) (File, error)
	DeleteFile(ctx context.Context, id, tripID string) (bool, error)

	CreateChecklist(ctx context.Context, p CreateChecklistParams) (Checklist, error)
	GetChecklistByID(ctx context.Context, id string) (Checklist, error)
	ListChecklistsByTrip(ctx context.Context, tripID string) ([]Checklist, error)
	DeleteChecklist(ctx context.Context, id, tripID string) (bool, error)

	CreateChecklistItem(ctx context.Context, p CreateChecklistItemParams) (ChecklistItem, error)
	ListChecklistItemsByChecklist(ctx context.Context, checklistID string) ([]ChecklistItem, error)
	SetChecklistItemChecked(ctx context.Context, id, checklistID string, checked bool) (ChecklistItem, error)
	DeleteChecklistItem(ctx context.Context, id, checklistID string) (bool, error)

	// WithTx runs fn with a Store bound to a single transaction, committing
	// on success and rolling back if fn returns an error.
	WithTx(ctx context.Context, fn func(Store) error) error
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
