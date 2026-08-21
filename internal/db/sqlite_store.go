package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	sqlitegen "caravel/internal/db/sqlc/sqlite/gen"
)

// timeLayout is used to store timestamps as sortable, unambiguous TEXT in
// SQLite. time.Parse(timeLayout, ...) also accepts values with no fractional
// seconds component, so it round-trips values written by either layout.
const timeLayout = time.RFC3339Nano

type sqliteStore struct {
	q  *sqlitegen.Queries
	db sqlitegen.DBTX
}

func newSQLiteStore(conn *sql.DB) *sqliteStore {
	return &sqliteStore{q: sqlitegen.New(conn), db: conn}
}

func formatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func parseTime(s string) time.Time {
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		// Should be unreachable: every write goes through formatTime, so a
		// parse failure here means the DB was written by something else.
		return time.Time{}
	}
	return t
}

func (s *sqliteStore) CreateUser(ctx context.Context, p CreateUserParams) (User, error) {
	row, err := s.q.CreateUser(ctx, sqlitegen.CreateUserParams{
		ID:          p.ID,
		Username:    p.Username,
		DisplayName: p.DisplayName,
		Email:       nullString(p.Email),
		CreatedAt:   formatTime(p.CreatedAt),
		UpdatedAt:   formatTime(p.UpdatedAt),
	})
	if err != nil {
		return User{}, err
	}
	return sqliteUserToDomain(row), nil
}

func (s *sqliteStore) GetUserByID(ctx context.Context, id string) (User, error) {
	row, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return User{}, mapNotFound(err)
	}
	return sqliteUserToDomain(row), nil
}

func (s *sqliteStore) GetUserByUsername(ctx context.Context, username string) (User, error) {
	row, err := s.q.GetUserByUsername(ctx, username)
	if err != nil {
		return User{}, mapNotFound(err)
	}
	return sqliteUserToDomain(row), nil
}

func (s *sqliteStore) CreateAuthIdentity(ctx context.Context, p CreateAuthIdentityParams) (AuthIdentity, error) {
	row, err := s.q.CreateAuthIdentity(ctx, sqlitegen.CreateAuthIdentityParams{
		ID:             p.ID,
		UserID:         p.UserID,
		Provider:       p.Provider,
		ProviderUserID: p.ProviderUserID,
		PasswordHash:   nullString(p.PasswordHash),
		CreatedAt:      formatTime(p.CreatedAt),
	})
	if err != nil {
		return AuthIdentity{}, err
	}
	return sqliteAuthIdentityToDomain(row), nil
}

func (s *sqliteStore) GetAuthIdentityByProvider(ctx context.Context, provider, providerUserID string) (AuthIdentity, error) {
	row, err := s.q.GetAuthIdentityByProvider(ctx, sqlitegen.GetAuthIdentityByProviderParams{
		Provider:       provider,
		ProviderUserID: providerUserID,
	})
	if err != nil {
		return AuthIdentity{}, mapNotFound(err)
	}
	return sqliteAuthIdentityToDomain(row), nil
}

func (s *sqliteStore) UpdateAuthIdentityPassword(ctx context.Context, provider, providerUserID, passwordHash string) error {
	return s.q.UpdateAuthIdentityPassword(ctx, sqlitegen.UpdateAuthIdentityPasswordParams{
		PasswordHash:   nullString(&passwordHash),
		Provider:       provider,
		ProviderUserID: providerUserID,
	})
}

func (s *sqliteStore) CreateSession(ctx context.Context, p CreateSessionParams) (Session, error) {
	row, err := s.q.CreateSession(ctx, sqlitegen.CreateSessionParams{
		ID:         p.ID,
		UserID:     p.UserID,
		CreatedAt:  formatTime(p.CreatedAt),
		ExpiresAt:  formatTime(p.ExpiresAt),
		LastSeenAt: formatTime(p.LastSeenAt),
		UserAgent:  nullString(p.UserAgent),
		Ip:         nullString(p.IP),
	})
	if err != nil {
		return Session{}, err
	}
	return sqliteSessionToDomain(row), nil
}

func (s *sqliteStore) GetSessionByID(ctx context.Context, id string) (Session, error) {
	row, err := s.q.GetSessionByID(ctx, id)
	if err != nil {
		return Session{}, mapNotFound(err)
	}
	return sqliteSessionToDomain(row), nil
}

func (s *sqliteStore) TouchSession(ctx context.Context, id string, lastSeenAt, expiresAt time.Time) error {
	return s.q.TouchSession(ctx, sqlitegen.TouchSessionParams{
		ID:         id,
		LastSeenAt: formatTime(lastSeenAt),
		ExpiresAt:  formatTime(expiresAt),
	})
}

func (s *sqliteStore) DeleteSession(ctx context.Context, id string) error {
	return s.q.DeleteSession(ctx, id)
}

func (s *sqliteStore) DeleteSessionsByUserID(ctx context.Context, userID string) error {
	return s.q.DeleteSessionsByUserID(ctx, userID)
}

func (s *sqliteStore) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	return s.q.DeleteExpiredSessions(ctx, formatTime(now))
}

func (s *sqliteStore) CreateTrip(ctx context.Context, p CreateTripParams) (Trip, error) {
	row, err := s.q.CreateTrip(ctx, sqlitegen.CreateTripParams{
		ID:        p.ID,
		OwnerID:   p.OwnerID,
		Title:     p.Title,
		StartDate: nullString(p.StartDate),
		EndDate:   nullString(p.EndDate),
		Subtitle:  nullString(p.Subtitle),
		CreatedAt: formatTime(p.CreatedAt),
		UpdatedAt: formatTime(p.UpdatedAt),
	})
	if err != nil {
		return Trip{}, err
	}
	return sqliteTripToDomain(row), nil
}

func (s *sqliteStore) GetTripByID(ctx context.Context, id string) (Trip, error) {
	row, err := s.q.GetTripByID(ctx, id)
	if err != nil {
		return Trip{}, mapNotFound(err)
	}
	return sqliteTripToDomain(row), nil
}

func (s *sqliteStore) ListTripsByOwner(ctx context.Context, ownerID string) ([]Trip, error) {
	rows, err := s.q.ListTripsByOwner(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	trips := make([]Trip, len(rows))
	for i, row := range rows {
		trips[i] = sqliteTripToDomain(row)
	}
	return trips, nil
}

func (s *sqliteStore) UpdateTrip(ctx context.Context, p UpdateTripParams) (Trip, error) {
	row, err := s.q.UpdateTrip(ctx, sqlitegen.UpdateTripParams{
		ID:        p.ID,
		Title:     p.Title,
		StartDate: nullString(p.StartDate),
		EndDate:   nullString(p.EndDate),
		Subtitle:  nullString(p.Subtitle),
		UpdatedAt: formatTime(p.UpdatedAt),
	})
	if err != nil {
		return Trip{}, mapNotFound(err)
	}
	return sqliteTripToDomain(row), nil
}

func (s *sqliteStore) DeleteTrip(ctx context.Context, id, ownerID string) (bool, error) {
	n, err := s.q.DeleteTrip(ctx, sqlitegen.DeleteTripParams{ID: id, OwnerID: ownerID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *sqliteStore) SetTripPreviewImage(ctx context.Context, id string, imageID *string, updatedAt time.Time) (Trip, error) {
	row, err := s.q.SetTripPreviewImage(ctx, sqlitegen.SetTripPreviewImageParams{
		ID:             id,
		PreviewImageID: nullString(imageID),
		UpdatedAt:      formatTime(updatedAt),
	})
	if err != nil {
		return Trip{}, mapNotFound(err)
	}
	return sqliteTripToDomain(row), nil
}

func (s *sqliteStore) GetTripMember(ctx context.Context, tripID, userID string) (TripMember, error) {
	row, err := s.q.GetTripMember(ctx, sqlitegen.GetTripMemberParams{TripID: tripID, UserID: userID})
	if err != nil {
		return TripMember{}, mapNotFound(err)
	}
	// GetTripMember does not join users, so the display fields stay empty here.
	// Callers that need them are listing people, and use ListTripMembers.
	return TripMember{
		TripID:    row.TripID,
		UserID:    row.UserID,
		Role:      TripRole(row.Role),
		CreatedAt: parseTime(row.CreatedAt),
	}, nil
}

func (s *sqliteStore) ListTripMembers(ctx context.Context, tripID string) ([]TripMember, error) {
	rows, err := s.q.ListTripMembers(ctx, tripID)
	if err != nil {
		return nil, err
	}
	members := make([]TripMember, len(rows))
	for i, row := range rows {
		members[i] = TripMember{
			TripID:      row.TripID,
			UserID:      row.UserID,
			Role:        TripRole(row.Role),
			CreatedAt:   parseTime(row.CreatedAt),
			Username:    row.Username,
			DisplayName: row.DisplayName,
		}
	}
	return members, nil
}

func (s *sqliteStore) UpsertTripMember(ctx context.Context, tripID, userID string, role TripRole, createdAt time.Time) (TripMember, error) {
	row, err := s.q.UpsertTripMember(ctx, sqlitegen.UpsertTripMemberParams{
		TripID:    tripID,
		UserID:    userID,
		Role:      string(role),
		CreatedAt: formatTime(createdAt),
	})
	if err != nil {
		return TripMember{}, err
	}
	return TripMember{
		TripID:    row.TripID,
		UserID:    row.UserID,
		Role:      TripRole(row.Role),
		CreatedAt: parseTime(row.CreatedAt),
	}, nil
}

func (s *sqliteStore) DeleteTripMember(ctx context.Context, tripID, userID string) (bool, error) {
	n, err := s.q.DeleteTripMember(ctx, sqlitegen.DeleteTripMemberParams{TripID: tripID, UserID: userID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *sqliteStore) CountTripMembers(ctx context.Context, tripID string) (int64, error) {
	return s.q.CountTripMembers(ctx, tripID)
}

func boolToInt64(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func (s *sqliteStore) CreateItem(ctx context.Context, p CreateItemParams) (Item, error) {
	row, err := s.q.CreateItem(ctx, sqlitegen.CreateItemParams{
		ID:        p.ID,
		TripID:    p.TripID,
		Category:  p.Category,
		Type:      p.Type,
		Title:     p.Title,
		Notes:     nullString(p.Notes),
		ShowOnMap: boolToInt64(p.ShowOnMap),
		SortOrder: int64(p.SortOrder),
		CreatedAt: formatTime(p.CreatedAt),
		UpdatedAt: formatTime(p.UpdatedAt),
	})
	if err != nil {
		return Item{}, err
	}
	return sqliteItemToDomain(row), nil
}

func (s *sqliteStore) GetItemByID(ctx context.Context, id string) (Item, error) {
	row, err := s.q.GetItemByID(ctx, id)
	if err != nil {
		return Item{}, mapNotFound(err)
	}
	return sqliteItemToDomain(row), nil
}

func (s *sqliteStore) ListItemsByTrip(ctx context.Context, tripID string, category *string) ([]Item, error) {
	rows, err := s.q.ListItemsByTrip(ctx, sqlitegen.ListItemsByTripParams{
		TripID:   tripID,
		Category: nullString(category),
	})
	if err != nil {
		return nil, err
	}
	items := make([]Item, len(rows))
	for i, row := range rows {
		items[i] = sqliteItemToDomain(row)
	}
	return items, nil
}

func (s *sqliteStore) UpdateItem(ctx context.Context, p UpdateItemParams) (Item, error) {
	row, err := s.q.UpdateItem(ctx, sqlitegen.UpdateItemParams{
		ID:        p.ID,
		TripID:    p.TripID,
		Category:  p.Category,
		Type:      p.Type,
		Title:     p.Title,
		Notes:     nullString(p.Notes),
		ShowOnMap: boolToInt64(p.ShowOnMap),
		SortOrder: int64(p.SortOrder),
		UpdatedAt: formatTime(p.UpdatedAt),
	})
	if err != nil {
		return Item{}, mapNotFound(err)
	}
	return sqliteItemToDomain(row), nil
}

func (s *sqliteStore) DeleteItem(ctx context.Context, id, tripID string) (bool, error) {
	n, err := s.q.DeleteItem(ctx, sqlitegen.DeleteItemParams{ID: id, TripID: tripID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *sqliteStore) SetItemImage(ctx context.Context, id, tripID string, imageID *string, updatedAt time.Time) (Item, error) {
	row, err := s.q.SetItemImage(ctx, sqlitegen.SetItemImageParams{
		ID:        id,
		TripID:    tripID,
		ImageID:   nullString(imageID),
		UpdatedAt: formatTime(updatedAt),
	})
	if err != nil {
		return Item{}, mapNotFound(err)
	}
	return sqliteItemToDomain(row), nil
}

func (s *sqliteStore) UpsertItemLocation(ctx context.Context, p UpsertItemLocationParams) (ItemLocation, error) {
	n, err := s.q.UpdateItemLocation(ctx, sqlitegen.UpdateItemLocationParams{
		ItemID:  p.ItemID,
		Lat:     nullFloat64(p.Lat),
		Lng:     nullFloat64(p.Lng),
		Address: nullString(p.Address),
	})
	if err != nil {
		return ItemLocation{}, err
	}
	if n > 0 {
		return s.GetItemLocationByItemID(ctx, p.ItemID)
	}

	row, err := s.q.InsertItemLocation(ctx, sqlitegen.InsertItemLocationParams{
		ID:      p.ID,
		ItemID:  p.ItemID,
		Lat:     nullFloat64(p.Lat),
		Lng:     nullFloat64(p.Lng),
		Address: nullString(p.Address),
	})
	if err != nil {
		return ItemLocation{}, err
	}
	return sqliteItemLocationToDomain(row), nil
}

func (s *sqliteStore) GetItemLocationByItemID(ctx context.Context, itemID string) (ItemLocation, error) {
	row, err := s.q.GetItemLocationByItemID(ctx, itemID)
	if err != nil {
		return ItemLocation{}, mapNotFound(err)
	}
	return sqliteItemLocationToDomain(row), nil
}

func (s *sqliteStore) CreateItemLink(ctx context.Context, p CreateItemLinkParams) (ItemLink, error) {
	row, err := s.q.CreateItemLink(ctx, sqlitegen.CreateItemLinkParams{
		ID:        p.ID,
		ItemID:    p.ItemID,
		Url:       p.URL,
		Label:     nullString(p.Label),
		SortOrder: int64(p.SortOrder),
	})
	if err != nil {
		return ItemLink{}, err
	}
	return sqliteItemLinkToDomain(row), nil
}

func (s *sqliteStore) ListItemLinksByItem(ctx context.Context, itemID string) ([]ItemLink, error) {
	rows, err := s.q.ListItemLinksByItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	links := make([]ItemLink, len(rows))
	for i, row := range rows {
		links[i] = sqliteItemLinkToDomain(row)
	}
	return links, nil
}

func (s *sqliteStore) DeleteItemLink(ctx context.Context, id, itemID string) (bool, error) {
	n, err := s.q.DeleteItemLink(ctx, sqlitegen.DeleteItemLinkParams{ID: id, ItemID: itemID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *sqliteStore) CreateItemDate(ctx context.Context, p CreateItemDateParams) (ItemDate, error) {
	row, err := s.q.CreateItemDate(ctx, sqlitegen.CreateItemDateParams{
		ID:        p.ID,
		ItemID:    p.ItemID,
		StartDate: nullString(p.StartDate),
		EndDate:   nullString(p.EndDate),
		Label:     nullString(p.Label),
		AllDay:    boolToInt64(p.AllDay),
		StartTime: nullString(p.StartTime),
		EndTime:   nullString(p.EndTime),
	})
	if err != nil {
		return ItemDate{}, err
	}
	return sqliteItemDateToDomain(row), nil
}

func (s *sqliteStore) ListItemDatesByItem(ctx context.Context, itemID string) ([]ItemDate, error) {
	rows, err := s.q.ListItemDatesByItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	dates := make([]ItemDate, len(rows))
	for i, row := range rows {
		dates[i] = sqliteItemDateToDomain(row)
	}
	return dates, nil
}

func (s *sqliteStore) DeleteItemDate(ctx context.Context, id, itemID string) (bool, error) {
	n, err := s.q.DeleteItemDate(ctx, sqlitegen.DeleteItemDateParams{ID: id, ItemID: itemID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *sqliteStore) CreateMediaAsset(ctx context.Context, p CreateMediaAssetParams) (MediaAsset, error) {
	row, err := s.q.CreateMediaAsset(ctx, sqlitegen.CreateMediaAssetParams{
		ID:          p.ID,
		TripID:      p.TripID,
		Kind:        p.Kind,
		StoragePath: nullString(p.StoragePath),
		ExternalUrl: nullString(p.ExternalURL),
		ContentType: nullString(p.ContentType),
		Width:       nullInt64(p.Width),
		Height:      nullInt64(p.Height),
		CreatedAt:   formatTime(p.CreatedAt),
	})
	if err != nil {
		return MediaAsset{}, err
	}
	return sqliteMediaAssetToDomain(row), nil
}

func (s *sqliteStore) GetMediaAssetByID(ctx context.Context, id string) (MediaAsset, error) {
	row, err := s.q.GetMediaAssetByID(ctx, id)
	if err != nil {
		return MediaAsset{}, mapNotFound(err)
	}
	return sqliteMediaAssetToDomain(row), nil
}

func sqliteMediaAssetToDomain(m sqlitegen.MediaAsset) MediaAsset {
	return MediaAsset{
		ID:          m.ID,
		TripID:      m.TripID,
		Kind:        m.Kind,
		StoragePath: strPtr(m.StoragePath),
		ExternalURL: strPtr(m.ExternalUrl),
		ContentType: strPtr(m.ContentType),
		Width:       intPtr64(m.Width),
		Height:      intPtr64(m.Height),
		CreatedAt:   parseTime(m.CreatedAt),
	}
}

func (s *sqliteStore) ListMapItems(ctx context.Context, tripID string) ([]MapItem, error) {
	rows, err := s.q.ListMapItemsByTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	items := make([]MapItem, 0, len(rows))
	for _, row := range rows {
		if row.ShowOnMap == 0 {
			continue
		}
		items = append(items, MapItem{
			ID:       row.ID,
			Category: row.Category,
			Title:    row.Title,
			Lat:      row.Lat.Float64,
			Lng:      row.Lng.Float64,
		})
	}
	return items, nil
}

func (s *sqliteStore) ListItemCoordinates(ctx context.Context, tripID string) ([]ItemCoordinate, error) {
	rows, err := s.q.ListItemLocationsByTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	out := make([]ItemCoordinate, 0, len(rows))
	for _, row := range rows {
		// The query already excludes NULL lat/lng, but the generated types are
		// still nullable because the columns are; a row that somehow arrived
		// without both is dropped rather than reported at 0,0.
		if !row.Lat.Valid || !row.Lng.Valid {
			continue
		}
		out = append(out, ItemCoordinate{ItemID: row.ItemID, Lat: row.Lat.Float64, Lng: row.Lng.Float64})
	}
	return out, nil
}

func (s *sqliteStore) UpsertItineraryDayNotes(ctx context.Context, newID, tripID, date string, notes *string) (ItineraryDay, error) {
	n, err := s.q.UpdateItineraryDayNotes(ctx, sqlitegen.UpdateItineraryDayNotesParams{
		TripID: tripID,
		Date:   date,
		Notes:  nullString(notes),
	})
	if err != nil {
		return ItineraryDay{}, err
	}
	if n > 0 {
		row, err := s.q.GetItineraryDayByTripAndDate(ctx, sqlitegen.GetItineraryDayByTripAndDateParams{TripID: tripID, Date: date})
		if err != nil {
			return ItineraryDay{}, err
		}
		return sqliteItineraryDayToDomain(row), nil
	}

	row, err := s.q.InsertItineraryDay(ctx, sqlitegen.InsertItineraryDayParams{
		ID:     newID,
		TripID: tripID,
		Date:   date,
		Notes:  nullString(notes),
	})
	if err != nil {
		return ItineraryDay{}, err
	}
	return sqliteItineraryDayToDomain(row), nil
}

func (s *sqliteStore) ListItineraryDaysByTrip(ctx context.Context, tripID string) ([]ItineraryDay, error) {
	rows, err := s.q.ListItineraryDaysByTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	days := make([]ItineraryDay, len(rows))
	for i, row := range rows {
		days[i] = sqliteItineraryDayToDomain(row)
	}
	return days, nil
}

func (s *sqliteStore) GetItineraryDayByID(ctx context.Context, id string) (ItineraryDay, error) {
	row, err := s.q.GetItineraryDayByID(ctx, id)
	if err != nil {
		return ItineraryDay{}, mapNotFound(err)
	}
	return sqliteItineraryDayToDomain(row), nil
}

func (s *sqliteStore) DeleteItineraryDay(ctx context.Context, id, tripID string) (bool, error) {
	n, err := s.q.DeleteItineraryDay(ctx, sqlitegen.DeleteItineraryDayParams{ID: id, TripID: tripID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *sqliteStore) CreateItineraryEntry(ctx context.Context, p CreateItineraryEntryParams) (ItineraryEntry, error) {
	row, err := s.q.CreateItineraryEntry(ctx, sqlitegen.CreateItineraryEntryParams{
		ID:             p.ID,
		ItineraryDayID: p.ItineraryDayID,
		ItemID:         p.ItemID,
		SortOrder:      int64(p.SortOrder),
		Note:           nullString(p.Note),
	})
	if err != nil {
		return ItineraryEntry{}, err
	}
	return sqliteItineraryEntryToDomain(row), nil
}

func (s *sqliteStore) ListItineraryEntriesByTrip(ctx context.Context, tripID string) ([]ItineraryEntryDetail, error) {
	rows, err := s.q.ListItineraryEntriesByTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	entries := make([]ItineraryEntryDetail, len(rows))
	for i, row := range rows {
		entries[i] = ItineraryEntryDetail{
			ItineraryEntry: ItineraryEntry{
				ID:             row.ID,
				ItineraryDayID: row.ItineraryDayID,
				ItemID:         row.ItemID,
				SortOrder:      int(row.SortOrder),
				Note:           strPtr(row.Note),
			},
			ItemTitle:    row.ItemTitle,
			ItemCategory: row.ItemCategory,
			ItemType:     row.ItemType,
			ItemImageID:  strPtr(row.ItemImageID),
		}
	}
	return entries, nil
}

func (s *sqliteStore) DeleteItineraryEntry(ctx context.Context, id, itineraryDayID string) (bool, error) {
	n, err := s.q.DeleteItineraryEntry(ctx, sqlitegen.DeleteItineraryEntryParams{ID: id, ItineraryDayID: itineraryDayID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func sqliteItineraryDayToDomain(d sqlitegen.ItineraryDay) ItineraryDay {
	return ItineraryDay{
		ID:     d.ID,
		TripID: d.TripID,
		Date:   d.Date,
		Notes:  strPtr(d.Notes),
	}
}

func sqliteItineraryEntryToDomain(e sqlitegen.ItineraryEntry) ItineraryEntry {
	return ItineraryEntry{
		ID:             e.ID,
		ItineraryDayID: e.ItineraryDayID,
		ItemID:         e.ItemID,
		SortOrder:      int(e.SortOrder),
		Note:           strPtr(e.Note),
	}
}

func (s *sqliteStore) CreateFile(ctx context.Context, p CreateFileParams) (File, error) {
	row, err := s.q.CreateFile(ctx, sqlitegen.CreateFileParams{
		ID:          p.ID,
		TripID:      p.TripID,
		ItemID:      nullString(p.ItemID),
		Filename:    p.Filename,
		StoragePath: p.StoragePath,
		ContentType: nullString(p.ContentType),
		SizeBytes:   p.SizeBytes,
		UploadedAt:  formatTime(p.UploadedAt),
		Note:        nullString(p.Note),
	})
	if err != nil {
		return File{}, err
	}
	return sqliteFileToDomain(row), nil
}

func (s *sqliteStore) GetFileByID(ctx context.Context, id string) (File, error) {
	row, err := s.q.GetFileByID(ctx, id)
	if err != nil {
		return File{}, mapNotFound(err)
	}
	return sqliteFileToDomain(row), nil
}

func (s *sqliteStore) ListTripFiles(ctx context.Context, tripID string) ([]FileDetail, error) {
	rows, err := s.q.ListTripFiles(ctx, tripID)
	if err != nil {
		return nil, err
	}
	files := make([]FileDetail, len(rows))
	for i, row := range rows {
		files[i] = FileDetail{
			// The joined row is its own generated struct, so this can't go
			// through sqliteFileToDomain like the other file queries.
			File: File{
				ID:          row.ID,
				TripID:      row.TripID,
				ItemID:      strPtr(row.ItemID),
				Filename:    row.Filename,
				StoragePath: row.StoragePath,
				ContentType: strPtr(row.ContentType),
				SizeBytes:   row.SizeBytes,
				UploadedAt:  parseTime(row.UploadedAt),
				Note:        strPtr(row.Note),
			},
			ItemTitle: strPtr(row.ItemTitle),
		}
	}
	return files, nil
}

func (s *sqliteStore) ListItemFiles(ctx context.Context, itemID string) ([]File, error) {
	rows, err := s.q.ListItemFiles(ctx, nullString(&itemID))
	if err != nil {
		return nil, err
	}
	files := make([]File, len(rows))
	for i, row := range rows {
		files[i] = sqliteFileToDomain(row)
	}
	return files, nil
}

func (s *sqliteStore) UpdateFileNote(ctx context.Context, id, tripID string, note *string) (File, error) {
	row, err := s.q.UpdateFileNote(ctx, sqlitegen.UpdateFileNoteParams{
		Note:   nullString(note),
		ID:     id,
		TripID: tripID,
	})
	if err != nil {
		return File{}, mapNotFound(err)
	}
	return sqliteFileToDomain(row), nil
}

func (s *sqliteStore) DeleteFile(ctx context.Context, id, tripID string) (bool, error) {
	n, err := s.q.DeleteFile(ctx, sqlitegen.DeleteFileParams{ID: id, TripID: tripID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *sqliteStore) CreateChecklist(ctx context.Context, p CreateChecklistParams) (Checklist, error) {
	row, err := s.q.CreateChecklist(ctx, sqlitegen.CreateChecklistParams{
		ID:        p.ID,
		TripID:    p.TripID,
		Title:     p.Title,
		SortOrder: int64(p.SortOrder),
		CreatedAt: formatTime(p.CreatedAt),
	})
	if err != nil {
		return Checklist{}, err
	}
	return sqliteChecklistToDomain(row), nil
}

func (s *sqliteStore) GetChecklistByID(ctx context.Context, id string) (Checklist, error) {
	row, err := s.q.GetChecklistByID(ctx, id)
	if err != nil {
		return Checklist{}, mapNotFound(err)
	}
	return sqliteChecklistToDomain(row), nil
}

func (s *sqliteStore) ListChecklistsByTrip(ctx context.Context, tripID string) ([]Checklist, error) {
	rows, err := s.q.ListChecklistsByTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	checklists := make([]Checklist, len(rows))
	for i, row := range rows {
		checklists[i] = sqliteChecklistToDomain(row)
	}
	return checklists, nil
}

func (s *sqliteStore) DeleteChecklist(ctx context.Context, id, tripID string) (bool, error) {
	n, err := s.q.DeleteChecklist(ctx, sqlitegen.DeleteChecklistParams{ID: id, TripID: tripID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *sqliteStore) CreateChecklistItem(ctx context.Context, p CreateChecklistItemParams) (ChecklistItem, error) {
	row, err := s.q.CreateChecklistItem(ctx, sqlitegen.CreateChecklistItemParams{
		ID:          p.ID,
		ChecklistID: p.ChecklistID,
		Text:        p.Text,
		Checked:     boolToInt64(p.Checked),
		SortOrder:   int64(p.SortOrder),
		CreatedAt:   formatTime(p.CreatedAt),
	})
	if err != nil {
		return ChecklistItem{}, err
	}
	return sqliteChecklistItemToDomain(row), nil
}

func (s *sqliteStore) ListChecklistItemsByChecklist(ctx context.Context, checklistID string) ([]ChecklistItem, error) {
	rows, err := s.q.ListChecklistItemsByChecklist(ctx, checklistID)
	if err != nil {
		return nil, err
	}
	items := make([]ChecklistItem, len(rows))
	for i, row := range rows {
		items[i] = sqliteChecklistItemToDomain(row)
	}
	return items, nil
}

func (s *sqliteStore) SetChecklistItemChecked(ctx context.Context, id, checklistID string, checked bool) (ChecklistItem, error) {
	row, err := s.q.SetChecklistItemChecked(ctx, sqlitegen.SetChecklistItemCheckedParams{
		ID:          id,
		ChecklistID: checklistID,
		Checked:     boolToInt64(checked),
	})
	if err != nil {
		return ChecklistItem{}, mapNotFound(err)
	}
	return sqliteChecklistItemToDomain(row), nil
}

func (s *sqliteStore) DeleteChecklistItem(ctx context.Context, id, checklistID string) (bool, error) {
	n, err := s.q.DeleteChecklistItem(ctx, sqlitegen.DeleteChecklistItemParams{ID: id, ChecklistID: checklistID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func sqliteChecklistToDomain(c sqlitegen.Checklist) Checklist {
	return Checklist{
		ID:        c.ID,
		TripID:    c.TripID,
		Title:     c.Title,
		SortOrder: int(c.SortOrder),
		CreatedAt: parseTime(c.CreatedAt),
	}
}

func sqliteChecklistItemToDomain(c sqlitegen.ChecklistItem) ChecklistItem {
	return ChecklistItem{
		ID:          c.ID,
		ChecklistID: c.ChecklistID,
		Text:        c.Text,
		Checked:     c.Checked != 0,
		SortOrder:   int(c.SortOrder),
		CreatedAt:   parseTime(c.CreatedAt),
	}
}

func sqliteFileToDomain(d sqlitegen.File) File {
	return File{
		ID:          d.ID,
		TripID:      d.TripID,
		ItemID:      strPtr(d.ItemID),
		Filename:    d.Filename,
		StoragePath: d.StoragePath,
		ContentType: strPtr(d.ContentType),
		SizeBytes:   d.SizeBytes,
		UploadedAt:  parseTime(d.UploadedAt),
		Note:        strPtr(d.Note),
	}
}

func (s *sqliteStore) WithTx(ctx context.Context, fn func(Store) error) error {
	conn, ok := s.db.(*sql.DB)
	if !ok {
		return errors.New("sqliteStore.WithTx: not backed by *sql.DB")
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	txStore := &sqliteStore{q: s.q.WithTx(tx), db: tx}
	if err := fn(txStore); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func sqliteUserToDomain(u sqlitegen.User) User {
	return User{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Email:       strPtr(u.Email),
		CreatedAt:   parseTime(u.CreatedAt),
		UpdatedAt:   parseTime(u.UpdatedAt),
	}
}

func sqliteAuthIdentityToDomain(a sqlitegen.AuthIdentity) AuthIdentity {
	return AuthIdentity{
		ID:             a.ID,
		UserID:         a.UserID,
		Provider:       a.Provider,
		ProviderUserID: a.ProviderUserID,
		PasswordHash:   strPtr(a.PasswordHash),
		CreatedAt:      parseTime(a.CreatedAt),
	}
}

func sqliteItemToDomain(i sqlitegen.Item) Item {
	return Item{
		ID:        i.ID,
		TripID:    i.TripID,
		Category:  i.Category,
		Type:      i.Type,
		Title:     i.Title,
		Notes:     strPtr(i.Notes),
		ImageID:   strPtr(i.ImageID),
		ShowOnMap: i.ShowOnMap != 0,
		SortOrder: int(i.SortOrder),
		CreatedAt: parseTime(i.CreatedAt),
		UpdatedAt: parseTime(i.UpdatedAt),
	}
}

func sqliteItemLocationToDomain(l sqlitegen.ItemLocation) ItemLocation {
	return ItemLocation{
		ID:      l.ID,
		ItemID:  l.ItemID,
		Lat:     floatPtr(l.Lat),
		Lng:     floatPtr(l.Lng),
		Address: strPtr(l.Address),
	}
}

func sqliteItemLinkToDomain(l sqlitegen.ItemLink) ItemLink {
	return ItemLink{
		ID:        l.ID,
		ItemID:    l.ItemID,
		URL:       l.Url,
		Label:     strPtr(l.Label),
		SortOrder: int(l.SortOrder),
	}
}

func sqliteItemDateToDomain(d sqlitegen.ItemDate) ItemDate {
	return ItemDate{
		ID:        d.ID,
		ItemID:    d.ItemID,
		StartDate: strPtr(d.StartDate),
		EndDate:   strPtr(d.EndDate),
		Label:     strPtr(d.Label),
		AllDay:    d.AllDay != 0,
		StartTime: strPtr(d.StartTime),
		EndTime:   strPtr(d.EndTime),
	}
}

func sqliteTripToDomain(t sqlitegen.Trip) Trip {
	return Trip{
		ID:             t.ID,
		OwnerID:        t.OwnerID,
		Title:          t.Title,
		StartDate:      strPtr(t.StartDate),
		EndDate:        strPtr(t.EndDate),
		PreviewImageID: strPtr(t.PreviewImageID),
		Subtitle:       strPtr(t.Subtitle),
		CreatedAt:      parseTime(t.CreatedAt),
		UpdatedAt:      parseTime(t.UpdatedAt),
	}
}

func sqliteSessionToDomain(s sqlitegen.Session) Session {
	return Session{
		ID:         s.ID,
		UserID:     s.UserID,
		CreatedAt:  parseTime(s.CreatedAt),
		ExpiresAt:  parseTime(s.ExpiresAt),
		LastSeenAt: parseTime(s.LastSeenAt),
		UserAgent:  strPtr(s.UserAgent),
		IP:         strPtr(s.Ip),
	}
}
