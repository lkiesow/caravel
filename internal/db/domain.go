package db

import "time"

// Domain types are dialect-agnostic. Both the sqlite and postgres Store
// implementations convert their sqlc-generated rows (which have divergent
// Go types per dialect — e.g. string vs time.Time timestamps, int64 vs bool)
// into these shared shapes, so callers never see dialect-specific types.

type User struct {
	ID          string
	Username    string
	DisplayName string
	Email       *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type AuthIdentity struct {
	ID             string
	UserID         string
	Provider       string
	ProviderUserID string
	PasswordHash   *string
	CreatedAt      time.Time
}

type Session struct {
	ID         string
	UserID     string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
	UserAgent  *string
	IP         *string
}

// Trip.StartDate/EndDate are plain "YYYY-MM-DD" strings, not time.Time —
// they're calendar dates with no associated time zone, and representing them
// as such avoids the two dialects picking different notional timestamps for
// the same date (sqlite stores DATE as TEXT already; postgres's DATE maps to
// sql.NullTime, always at midnight UTC, which the store layer formats back
// down to the same "YYYY-MM-DD" shape).
type Trip struct {
	ID             string
	OwnerID        string
	Title          string
	StartDate      *string
	EndDate        *string
	PreviewImageID *string
	Notes          *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// MapItem is a lightweight projection for the map view: items with a
// resolvable location and show_on_map=true. Lat/Lng are always present here
// (the query only returns items with non-null coordinates).
type MapItem struct {
	ID       string
	Category string
	Title    string
	Lat      float64
	Lng      float64
}

// Document.ItemID is nil for trip-level "general documents" and set for
// documents attached to a specific item — see plan Section 2.2.
type Document struct {
	ID          string
	TripID      string
	ItemID      *string
	Filename    string
	StoragePath string
	ContentType *string
	SizeBytes   int64
	UploadedAt  time.Time
}

// ItineraryDay.Date is a "YYYY-MM-DD" string — see the note on
// Trip.StartDate/EndDate above; the same reasoning applies here.
type ItineraryDay struct {
	ID     string
	TripID string
	Date   string
	Notes  *string
}

type ItineraryEntry struct {
	ID             string
	ItineraryDayID string
	ItemID         string
	SortOrder      int
	Note           *string
}

// ItineraryEntryDetail is a lightweight join projection: an entry plus the
// summary fields of the item it references, for rendering the itinerary
// without a separate fetch per entry.
type ItineraryEntryDetail struct {
	ItineraryEntry
	ItemTitle    string
	ItemCategory string
	ItemType     string
}

// MediaAsset backs both trips.preview_image_id and items.image_id. Kind is
// "upload" (StoragePath set, resolved via internal/storagefs) or "url"
// (ExternalURL set, used directly — see Section 3.4 of the plan).
type MediaAsset struct {
	ID          string
	TripID      string
	Kind        string
	StoragePath *string
	ExternalURL *string
	ContentType *string
	Width       *int
	Height      *int
	CreatedAt   time.Time
}

type Item struct {
	ID        string
	TripID    string
	Category  string // "location" | "stay" | "transport"
	Type      string // free-text tag, e.g. "mountain", "hotel" — not a rigid enum
	Title     string
	Notes     *string
	ImageID   *string
	ShowOnMap bool
	SortOrder int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ItemLocation struct {
	ID      string
	ItemID  string
	Lat     *float64
	Lng     *float64
	Address *string
}

type ItemLink struct {
	ID        string
	ItemID    string
	URL       string
	Label     *string
	SortOrder int
}

// ItemDate.StartDate/EndDate are "YYYY-MM-DD" strings — see the note on
// Trip.StartDate/EndDate above; the same reasoning applies here.
type ItemDate struct {
	ID        string
	ItemID    string
	StartDate *string
	EndDate   *string
	Label     *string
	AllDay    bool
	StartTime *string
	EndTime   *string
}
