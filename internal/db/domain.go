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
	// IsAdmin governs *account* administration only — creating and removing
	// users, resetting passwords, opening registration. It grants no access to
	// anyone else's trips: httpapi.Server.tripRole never consults it, because a
	// "personal" file the instance operator can read is not a personal file.
	IsAdmin   bool
	CreatedAt time.Time
	UpdatedAt time.Time
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
	Subtitle       *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// TripRole is what a user may do on one trip. The three values are ordered —
// owner outranks editor outranks viewer — and AtLeast is the only comparison
// callers should use, so the ordering lives here rather than being re-derived
// at each authorization site.
//
// Only editor and viewer are ever *stored* (trip_members.role has a CHECK
// constraint to that effect); owner is derived from trips.owner_id, so there is
// exactly one owner per trip by construction and no row can claim to be one.
type TripRole string

const (
	RoleViewer TripRole = "viewer"
	RoleEditor TripRole = "editor"
	RoleOwner  TripRole = "owner"
)

// roleRank orders the roles. A role absent from this map ranks 0, which
// AtLeast treats as insufficient for everything — so a corrupted or
// future-dialect value fails closed rather than being read as an owner.
var roleRank = map[TripRole]int{
	RoleViewer: 1,
	RoleEditor: 2,
	RoleOwner:  3,
}

// AtLeast reports whether r carries at least the authority of min.
func (r TripRole) AtLeast(min TripRole) bool {
	return roleRank[r] >= roleRank[min] && roleRank[r] > 0
}

// Valid reports whether r is one of the three known roles. Used when accepting
// a role from a request body.
func (r TripRole) Valid() bool { return roleRank[r] > 0 }

// Assignable reports whether r is a role that can be *given* to someone on a
// trip. Owner is not assignable: it belongs to trips.owner_id, and handing it
// out here would create a second one.
func (r TripRole) Assignable() bool { return r == RoleEditor || r == RoleViewer }

// TripMember is one non-owner's membership of a trip, with the user's own
// display fields joined in — the members list is a list of people, so the two
// always travel together.
type TripMember struct {
	TripID      string
	UserID      string
	Role        TripRole
	CreatedAt   time.Time
	Username    string
	DisplayName string
}

// UserSummary is the subset of a user safe to hand to anyone searching for
// someone to share a trip with: enough to recognise a person, and nothing else.
// Notably no email and no timestamps.
type UserSummary struct {
	ID          string
	Username    string
	DisplayName string
}

// UserWithTripCount is a user as the admin screen lists them, with the number
// of trips they own. Owned, not accessible: the count exists to answer what
// deleting this account would destroy, and trips merely shared with them belong
// to somebody else.
type UserWithTripCount struct {
	User
	TripCount int64
}

// TripForUser is a trip together with the reading user's own relationship to
// it: their role, and who owns it. The trips list needs both for every row, and
// getting them from the same query is what keeps that endpoint at one round
// trip rather than one per trip.
type TripForUser struct {
	Trip
	Role             TripRole
	OwnerUsername    string
	OwnerDisplayName string
	// MemberCount counts non-owner members, so zero means a solo trip. The
	// client uses it to decide whether per-file visibility is a question worth
	// asking at all.
	MemberCount int64
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

// ItemCoordinate is one item's position, for callers that need coordinates
// across a whole trip without the rest of each item. Unlike MapItem it
// ignores show_on_map — that flag governs whether a place is *drawn* on the
// map, not whether it has a position — and only rows with both values set are
// returned, so "has no coordinates" never arrives disguised as 0,0.
type ItemCoordinate struct {
	ItemID string
	Lat    float64
	Lng    float64
}

// File.ItemID is nil for trip-level "general files" and set for
// files attached to a specific item — see plan Section 2.2.
// FileVisibility is who can see a file on a shared trip. Two values only, and
// the reason there is no third is worth knowing: checklists get a `shared`
// state because being *ticked* is a second axis, and a file has no equivalent —
// who may edit its note or delete it already follows the trip role.
type FileVisibility string

const (
	// FileVisibilityPersonal: only whoever uploaded it. The boarding-pass case.
	FileVisibilityPersonal FileVisibility = "personal"
	// FileVisibilityTrip: everyone who can see the trip. The default.
	FileVisibilityTrip FileVisibility = "trip"
)

// Valid reports whether v is a storable visibility, for accepting one from a
// request body.
func (v FileVisibility) Valid() bool {
	return v == FileVisibilityPersonal || v == FileVisibilityTrip
}

type File struct {
	ID          string
	TripID      string
	ItemID      *string
	Filename    string
	StoragePath string
	ContentType *string
	SizeBytes   int64
	UploadedAt  time.Time
	Note        *string
	Visibility  FileVisibility
	// OwnerUserID is who uploaded it, and therefore whose file a personal one
	// is. A pointer because the column is nullable — see migration 0009; in
	// practice every row has one.
	OwnerUserID *string
}

// FileDetail is a file plus the title of the location it is attached
// to, for the trip-level Files list: that list mixes trip-level files with
// location-attached ones, and a filename alone doesn't say which location a
// file belongs to. ItemTitle is nil exactly when ItemID is - i.e. for a
// trip-level file - so the two are read together, and it is a *join
// projection*, not a field of File: nothing writes it.
//
// Same shape as ItineraryEntryDetail below, for the same reason.
type FileDetail struct {
	File
	ItemTitle *string
}

// ChecklistVisibility is who can see and who can tick a checklist. Three
// values where a file has two, because a checklist has a second axis a file
// does not: being able to *change* it is separate from being able to read it
// once more than one person is looking.
type ChecklistVisibility string

const (
	// ChecklistPersonal: only its author sees it at all.
	ChecklistPersonal ChecklistVisibility = "personal"
	// ChecklistTrip: everyone on the trip sees it, only its author changes it.
	ChecklistTrip ChecklistVisibility = "trip"
	// ChecklistShared: everyone on the trip sees it and ticks it. The default,
	// because a checklist is usually a job being done together — which is the
	// opposite of the files default, where a document is usually one person's.
	ChecklistShared ChecklistVisibility = "shared"
)

func (v ChecklistVisibility) Valid() bool {
	return v == ChecklistPersonal || v == ChecklistTrip || v == ChecklistShared
}

type Checklist struct {
	ID         string
	TripID     string
	Title      string
	SortOrder  int
	CreatedAt  time.Time
	Visibility ChecklistVisibility
	// OwnerUserID is who created it. Nullable in the schema (see migration
	// 0010); in practice every row has one.
	OwnerUserID *string
}

type ChecklistItem struct {
	ID          string
	ChecklistID string
	Text        string
	Checked     bool
	SortOrder   int
	CreatedAt   time.Time
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
	ItemImageID  *string
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
