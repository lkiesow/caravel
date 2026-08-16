package db

import (
	"context"
	"database/sql"
	"errors"
	"time"

	postgresgen "caravel/internal/db/sqlc/postgres/gen"
)

type postgresStore struct {
	q  *postgresgen.Queries
	db postgresgen.DBTX
}

func newPostgresStore(conn *sql.DB) *postgresStore {
	return &postgresStore{q: postgresgen.New(conn), db: conn}
}

func (s *postgresStore) CreateUser(ctx context.Context, p CreateUserParams) (User, error) {
	row, err := s.q.CreateUser(ctx, postgresgen.CreateUserParams{
		ID:          p.ID,
		Username:    p.Username,
		DisplayName: p.DisplayName,
		Email:       nullString(p.Email),
		CreatedAt:   p.CreatedAt.UTC(),
		UpdatedAt:   p.UpdatedAt.UTC(),
	})
	if err != nil {
		return User{}, err
	}
	return postgresUserToDomain(row), nil
}

func (s *postgresStore) GetUserByID(ctx context.Context, id string) (User, error) {
	row, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		return User{}, mapNotFound(err)
	}
	return postgresUserToDomain(row), nil
}

func (s *postgresStore) GetUserByUsername(ctx context.Context, username string) (User, error) {
	row, err := s.q.GetUserByUsername(ctx, username)
	if err != nil {
		return User{}, mapNotFound(err)
	}
	return postgresUserToDomain(row), nil
}

func (s *postgresStore) CreateAuthIdentity(ctx context.Context, p CreateAuthIdentityParams) (AuthIdentity, error) {
	row, err := s.q.CreateAuthIdentity(ctx, postgresgen.CreateAuthIdentityParams{
		ID:             p.ID,
		UserID:         p.UserID,
		Provider:       p.Provider,
		ProviderUserID: p.ProviderUserID,
		PasswordHash:   nullString(p.PasswordHash),
		CreatedAt:      p.CreatedAt.UTC(),
	})
	if err != nil {
		return AuthIdentity{}, err
	}
	return postgresAuthIdentityToDomain(row), nil
}

func (s *postgresStore) GetAuthIdentityByProvider(ctx context.Context, provider, providerUserID string) (AuthIdentity, error) {
	row, err := s.q.GetAuthIdentityByProvider(ctx, postgresgen.GetAuthIdentityByProviderParams{
		Provider:       provider,
		ProviderUserID: providerUserID,
	})
	if err != nil {
		return AuthIdentity{}, mapNotFound(err)
	}
	return postgresAuthIdentityToDomain(row), nil
}

func (s *postgresStore) CreateSession(ctx context.Context, p CreateSessionParams) (Session, error) {
	row, err := s.q.CreateSession(ctx, postgresgen.CreateSessionParams{
		ID:         p.ID,
		UserID:     p.UserID,
		CreatedAt:  p.CreatedAt.UTC(),
		ExpiresAt:  p.ExpiresAt.UTC(),
		LastSeenAt: p.LastSeenAt.UTC(),
		UserAgent:  nullString(p.UserAgent),
		Ip:         nullString(p.IP),
	})
	if err != nil {
		return Session{}, err
	}
	return postgresSessionToDomain(row), nil
}

func (s *postgresStore) GetSessionByID(ctx context.Context, id string) (Session, error) {
	row, err := s.q.GetSessionByID(ctx, id)
	if err != nil {
		return Session{}, mapNotFound(err)
	}
	return postgresSessionToDomain(row), nil
}

func (s *postgresStore) TouchSession(ctx context.Context, id string, lastSeenAt, expiresAt time.Time) error {
	return s.q.TouchSession(ctx, postgresgen.TouchSessionParams{
		ID:         id,
		LastSeenAt: lastSeenAt.UTC(),
		ExpiresAt:  expiresAt.UTC(),
	})
}

func (s *postgresStore) DeleteSession(ctx context.Context, id string) error {
	return s.q.DeleteSession(ctx, id)
}

func (s *postgresStore) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	return s.q.DeleteExpiredSessions(ctx, now.UTC())
}

const dateLayout = "2006-01-02"

func (s *postgresStore) CreateItem(ctx context.Context, p CreateItemParams) (Item, error) {
	row, err := s.q.CreateItem(ctx, postgresgen.CreateItemParams{
		ID:        p.ID,
		TripID:    p.TripID,
		Category:  p.Category,
		Type:      p.Type,
		Title:     p.Title,
		Notes:     nullString(p.Notes),
		ShowOnMap: p.ShowOnMap,
		SortOrder: int32(p.SortOrder),
		CreatedAt: p.CreatedAt.UTC(),
		UpdatedAt: p.UpdatedAt.UTC(),
	})
	if err != nil {
		return Item{}, err
	}
	return postgresItemToDomain(row), nil
}

func (s *postgresStore) GetItemByID(ctx context.Context, id string) (Item, error) {
	row, err := s.q.GetItemByID(ctx, id)
	if err != nil {
		return Item{}, mapNotFound(err)
	}
	return postgresItemToDomain(row), nil
}

func (s *postgresStore) ListItemsByTrip(ctx context.Context, tripID string, category *string) ([]Item, error) {
	rows, err := s.q.ListItemsByTrip(ctx, postgresgen.ListItemsByTripParams{
		TripID:   tripID,
		Category: nullString(category),
	})
	if err != nil {
		return nil, err
	}
	items := make([]Item, len(rows))
	for i, row := range rows {
		items[i] = postgresItemToDomain(row)
	}
	return items, nil
}

func (s *postgresStore) UpdateItem(ctx context.Context, p UpdateItemParams) (Item, error) {
	row, err := s.q.UpdateItem(ctx, postgresgen.UpdateItemParams{
		ID:        p.ID,
		TripID:    p.TripID,
		Category:  p.Category,
		Type:      p.Type,
		Title:     p.Title,
		Notes:     nullString(p.Notes),
		ShowOnMap: p.ShowOnMap,
		SortOrder: int32(p.SortOrder),
		UpdatedAt: p.UpdatedAt.UTC(),
	})
	if err != nil {
		return Item{}, mapNotFound(err)
	}
	return postgresItemToDomain(row), nil
}

func (s *postgresStore) DeleteItem(ctx context.Context, id, tripID string) (bool, error) {
	n, err := s.q.DeleteItem(ctx, postgresgen.DeleteItemParams{ID: id, TripID: tripID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *postgresStore) SetItemImage(ctx context.Context, id, tripID string, imageID *string, updatedAt time.Time) (Item, error) {
	row, err := s.q.SetItemImage(ctx, postgresgen.SetItemImageParams{
		ID:        id,
		TripID:    tripID,
		ImageID:   nullString(imageID),
		UpdatedAt: updatedAt.UTC(),
	})
	if err != nil {
		return Item{}, mapNotFound(err)
	}
	return postgresItemToDomain(row), nil
}

func (s *postgresStore) UpsertItemLocation(ctx context.Context, p UpsertItemLocationParams) (ItemLocation, error) {
	n, err := s.q.UpdateItemLocation(ctx, postgresgen.UpdateItemLocationParams{
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

	row, err := s.q.InsertItemLocation(ctx, postgresgen.InsertItemLocationParams{
		ID:      p.ID,
		ItemID:  p.ItemID,
		Lat:     nullFloat64(p.Lat),
		Lng:     nullFloat64(p.Lng),
		Address: nullString(p.Address),
	})
	if err != nil {
		return ItemLocation{}, err
	}
	return postgresItemLocationToDomain(row), nil
}

func (s *postgresStore) GetItemLocationByItemID(ctx context.Context, itemID string) (ItemLocation, error) {
	row, err := s.q.GetItemLocationByItemID(ctx, itemID)
	if err != nil {
		return ItemLocation{}, mapNotFound(err)
	}
	return postgresItemLocationToDomain(row), nil
}

func (s *postgresStore) CreateItemLink(ctx context.Context, p CreateItemLinkParams) (ItemLink, error) {
	row, err := s.q.CreateItemLink(ctx, postgresgen.CreateItemLinkParams{
		ID:        p.ID,
		ItemID:    p.ItemID,
		Url:       p.URL,
		Label:     nullString(p.Label),
		SortOrder: int32(p.SortOrder),
	})
	if err != nil {
		return ItemLink{}, err
	}
	return postgresItemLinkToDomain(row), nil
}

func (s *postgresStore) ListItemLinksByItem(ctx context.Context, itemID string) ([]ItemLink, error) {
	rows, err := s.q.ListItemLinksByItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	links := make([]ItemLink, len(rows))
	for i, row := range rows {
		links[i] = postgresItemLinkToDomain(row)
	}
	return links, nil
}

func (s *postgresStore) DeleteItemLink(ctx context.Context, id, itemID string) (bool, error) {
	n, err := s.q.DeleteItemLink(ctx, postgresgen.DeleteItemLinkParams{ID: id, ItemID: itemID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *postgresStore) CreateItemDate(ctx context.Context, p CreateItemDateParams) (ItemDate, error) {
	row, err := s.q.CreateItemDate(ctx, postgresgen.CreateItemDateParams{
		ID:        p.ID,
		ItemID:    p.ItemID,
		StartDate: nullDate(p.StartDate),
		EndDate:   nullDate(p.EndDate),
		Label:     nullString(p.Label),
		AllDay:    p.AllDay,
		StartTime: nullString(p.StartTime),
		EndTime:   nullString(p.EndTime),
	})
	if err != nil {
		return ItemDate{}, err
	}
	return postgresItemDateToDomain(row), nil
}

func (s *postgresStore) ListItemDatesByItem(ctx context.Context, itemID string) ([]ItemDate, error) {
	rows, err := s.q.ListItemDatesByItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	dates := make([]ItemDate, len(rows))
	for i, row := range rows {
		dates[i] = postgresItemDateToDomain(row)
	}
	return dates, nil
}

func (s *postgresStore) DeleteItemDate(ctx context.Context, id, itemID string) (bool, error) {
	n, err := s.q.DeleteItemDate(ctx, postgresgen.DeleteItemDateParams{ID: id, ItemID: itemID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func postgresItemToDomain(i postgresgen.Item) Item {
	return Item{
		ID:        i.ID,
		TripID:    i.TripID,
		Category:  i.Category,
		Type:      i.Type,
		Title:     i.Title,
		Notes:     strPtr(i.Notes),
		ImageID:   strPtr(i.ImageID),
		ShowOnMap: i.ShowOnMap,
		SortOrder: int(i.SortOrder),
		CreatedAt: i.CreatedAt,
		UpdatedAt: i.UpdatedAt,
	}
}

func postgresItemLocationToDomain(l postgresgen.ItemLocation) ItemLocation {
	return ItemLocation{
		ID:      l.ID,
		ItemID:  l.ItemID,
		Lat:     floatPtr(l.Lat),
		Lng:     floatPtr(l.Lng),
		Address: strPtr(l.Address),
	}
}

func postgresItemLinkToDomain(l postgresgen.ItemLink) ItemLink {
	return ItemLink{
		ID:        l.ID,
		ItemID:    l.ItemID,
		URL:       l.Url,
		Label:     strPtr(l.Label),
		SortOrder: int(l.SortOrder),
	}
}

func postgresItemDateToDomain(d postgresgen.ItemDate) ItemDate {
	return ItemDate{
		ID:        d.ID,
		ItemID:    d.ItemID,
		StartDate: datePtr(d.StartDate),
		EndDate:   datePtr(d.EndDate),
		Label:     strPtr(d.Label),
		AllDay:    d.AllDay,
		StartTime: strPtr(d.StartTime),
		EndTime:   strPtr(d.EndTime),
	}
}

func nullDate(p *string) sql.NullTime {
	if p == nil {
		return sql.NullTime{}
	}
	t, err := time.Parse(dateLayout, *p)
	if err != nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

func datePtr(nt sql.NullTime) *string {
	if !nt.Valid {
		return nil
	}
	v := nt.Time.Format(dateLayout)
	return &v
}

func (s *postgresStore) CreateTrip(ctx context.Context, p CreateTripParams) (Trip, error) {
	row, err := s.q.CreateTrip(ctx, postgresgen.CreateTripParams{
		ID:        p.ID,
		OwnerID:   p.OwnerID,
		Title:     p.Title,
		StartDate: nullDate(p.StartDate),
		EndDate:   nullDate(p.EndDate),
		Subtitle:  nullString(p.Subtitle),
		CreatedAt: p.CreatedAt.UTC(),
		UpdatedAt: p.UpdatedAt.UTC(),
	})
	if err != nil {
		return Trip{}, err
	}
	return postgresTripToDomain(row), nil
}

func (s *postgresStore) GetTripByID(ctx context.Context, id string) (Trip, error) {
	row, err := s.q.GetTripByID(ctx, id)
	if err != nil {
		return Trip{}, mapNotFound(err)
	}
	return postgresTripToDomain(row), nil
}

func (s *postgresStore) ListTripsByOwner(ctx context.Context, ownerID string) ([]Trip, error) {
	rows, err := s.q.ListTripsByOwner(ctx, ownerID)
	if err != nil {
		return nil, err
	}
	trips := make([]Trip, len(rows))
	for i, row := range rows {
		trips[i] = postgresTripToDomain(row)
	}
	return trips, nil
}

func (s *postgresStore) UpdateTrip(ctx context.Context, p UpdateTripParams) (Trip, error) {
	row, err := s.q.UpdateTrip(ctx, postgresgen.UpdateTripParams{
		ID:        p.ID,
		OwnerID:   p.OwnerID,
		Title:     p.Title,
		StartDate: nullDate(p.StartDate),
		EndDate:   nullDate(p.EndDate),
		Subtitle:  nullString(p.Subtitle),
		UpdatedAt: p.UpdatedAt.UTC(),
	})
	if err != nil {
		return Trip{}, mapNotFound(err)
	}
	return postgresTripToDomain(row), nil
}

func (s *postgresStore) DeleteTrip(ctx context.Context, id, ownerID string) (bool, error) {
	n, err := s.q.DeleteTrip(ctx, postgresgen.DeleteTripParams{ID: id, OwnerID: ownerID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *postgresStore) SetTripPreviewImage(ctx context.Context, id, ownerID string, imageID *string, updatedAt time.Time) (Trip, error) {
	row, err := s.q.SetTripPreviewImage(ctx, postgresgen.SetTripPreviewImageParams{
		ID:             id,
		OwnerID:        ownerID,
		PreviewImageID: nullString(imageID),
		UpdatedAt:      updatedAt.UTC(),
	})
	if err != nil {
		return Trip{}, mapNotFound(err)
	}
	return postgresTripToDomain(row), nil
}

func (s *postgresStore) CreateMediaAsset(ctx context.Context, p CreateMediaAssetParams) (MediaAsset, error) {
	row, err := s.q.CreateMediaAsset(ctx, postgresgen.CreateMediaAssetParams{
		ID:          p.ID,
		TripID:      p.TripID,
		Kind:        p.Kind,
		StoragePath: nullString(p.StoragePath),
		ExternalUrl: nullString(p.ExternalURL),
		ContentType: nullString(p.ContentType),
		Width:       nullInt32(p.Width),
		Height:      nullInt32(p.Height),
		CreatedAt:   p.CreatedAt.UTC(),
	})
	if err != nil {
		return MediaAsset{}, err
	}
	return postgresMediaAssetToDomain(row), nil
}

func (s *postgresStore) GetMediaAssetByID(ctx context.Context, id string) (MediaAsset, error) {
	row, err := s.q.GetMediaAssetByID(ctx, id)
	if err != nil {
		return MediaAsset{}, mapNotFound(err)
	}
	return postgresMediaAssetToDomain(row), nil
}

func postgresMediaAssetToDomain(m postgresgen.MediaAsset) MediaAsset {
	return MediaAsset{
		ID:          m.ID,
		TripID:      m.TripID,
		Kind:        m.Kind,
		StoragePath: strPtr(m.StoragePath),
		ExternalURL: strPtr(m.ExternalUrl),
		ContentType: strPtr(m.ContentType),
		Width:       intPtr32(m.Width),
		Height:      intPtr32(m.Height),
		CreatedAt:   m.CreatedAt,
	}
}

func postgresTripToDomain(t postgresgen.Trip) Trip {
	return Trip{
		ID:             t.ID,
		OwnerID:        t.OwnerID,
		Title:          t.Title,
		StartDate:      datePtr(t.StartDate),
		EndDate:        datePtr(t.EndDate),
		PreviewImageID: strPtr(t.PreviewImageID),
		Subtitle:       strPtr(t.Subtitle),
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
	}
}

func (s *postgresStore) ListMapItems(ctx context.Context, tripID string) ([]MapItem, error) {
	rows, err := s.q.ListMapItemsByTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	items := make([]MapItem, 0, len(rows))
	for _, row := range rows {
		if !row.ShowOnMap {
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

func (s *postgresStore) UpsertItineraryDayNotes(ctx context.Context, newID, tripID, date string, notes *string) (ItineraryDay, error) {
	parsedDate, err := time.Parse(dateLayout, date)
	if err != nil {
		return ItineraryDay{}, err
	}

	n, err := s.q.UpdateItineraryDayNotes(ctx, postgresgen.UpdateItineraryDayNotesParams{
		TripID: tripID,
		Date:   parsedDate,
		Notes:  nullString(notes),
	})
	if err != nil {
		return ItineraryDay{}, err
	}
	if n > 0 {
		row, err := s.q.GetItineraryDayByTripAndDate(ctx, postgresgen.GetItineraryDayByTripAndDateParams{TripID: tripID, Date: parsedDate})
		if err != nil {
			return ItineraryDay{}, err
		}
		return postgresItineraryDayToDomain(row), nil
	}

	row, err := s.q.InsertItineraryDay(ctx, postgresgen.InsertItineraryDayParams{
		ID:     newID,
		TripID: tripID,
		Date:   parsedDate,
		Notes:  nullString(notes),
	})
	if err != nil {
		return ItineraryDay{}, err
	}
	return postgresItineraryDayToDomain(row), nil
}

func (s *postgresStore) ListItineraryDaysByTrip(ctx context.Context, tripID string) ([]ItineraryDay, error) {
	rows, err := s.q.ListItineraryDaysByTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	days := make([]ItineraryDay, len(rows))
	for i, row := range rows {
		days[i] = postgresItineraryDayToDomain(row)
	}
	return days, nil
}

func (s *postgresStore) GetItineraryDayByID(ctx context.Context, id string) (ItineraryDay, error) {
	row, err := s.q.GetItineraryDayByID(ctx, id)
	if err != nil {
		return ItineraryDay{}, mapNotFound(err)
	}
	return postgresItineraryDayToDomain(row), nil
}

func (s *postgresStore) DeleteItineraryDay(ctx context.Context, id, tripID string) (bool, error) {
	n, err := s.q.DeleteItineraryDay(ctx, postgresgen.DeleteItineraryDayParams{ID: id, TripID: tripID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *postgresStore) CreateItineraryEntry(ctx context.Context, p CreateItineraryEntryParams) (ItineraryEntry, error) {
	row, err := s.q.CreateItineraryEntry(ctx, postgresgen.CreateItineraryEntryParams{
		ID:             p.ID,
		ItineraryDayID: p.ItineraryDayID,
		ItemID:         p.ItemID,
		SortOrder:      int32(p.SortOrder),
		Note:           nullString(p.Note),
	})
	if err != nil {
		return ItineraryEntry{}, err
	}
	return postgresItineraryEntryToDomain(row), nil
}

func (s *postgresStore) ListItineraryEntriesByTrip(ctx context.Context, tripID string) ([]ItineraryEntryDetail, error) {
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

func (s *postgresStore) DeleteItineraryEntry(ctx context.Context, id, itineraryDayID string) (bool, error) {
	n, err := s.q.DeleteItineraryEntry(ctx, postgresgen.DeleteItineraryEntryParams{ID: id, ItineraryDayID: itineraryDayID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func postgresItineraryDayToDomain(d postgresgen.ItineraryDay) ItineraryDay {
	return ItineraryDay{
		ID:     d.ID,
		TripID: d.TripID,
		Date:   d.Date.Format(dateLayout),
		Notes:  strPtr(d.Notes),
	}
}

func postgresItineraryEntryToDomain(e postgresgen.ItineraryEntry) ItineraryEntry {
	return ItineraryEntry{
		ID:             e.ID,
		ItineraryDayID: e.ItineraryDayID,
		ItemID:         e.ItemID,
		SortOrder:      int(e.SortOrder),
		Note:           strPtr(e.Note),
	}
}

func (s *postgresStore) CreateDocument(ctx context.Context, p CreateDocumentParams) (Document, error) {
	row, err := s.q.CreateDocument(ctx, postgresgen.CreateDocumentParams{
		ID:          p.ID,
		TripID:      p.TripID,
		ItemID:      nullString(p.ItemID),
		Filename:    p.Filename,
		StoragePath: p.StoragePath,
		ContentType: nullString(p.ContentType),
		SizeBytes:   p.SizeBytes,
		UploadedAt:  p.UploadedAt.UTC(),
		Note:        nullString(p.Note),
	})
	if err != nil {
		return Document{}, err
	}
	return postgresDocumentToDomain(row), nil
}

func (s *postgresStore) GetDocumentByID(ctx context.Context, id string) (Document, error) {
	row, err := s.q.GetDocumentByID(ctx, id)
	if err != nil {
		return Document{}, mapNotFound(err)
	}
	return postgresDocumentToDomain(row), nil
}

func (s *postgresStore) ListTripDocuments(ctx context.Context, tripID string) ([]Document, error) {
	rows, err := s.q.ListTripDocuments(ctx, tripID)
	if err != nil {
		return nil, err
	}
	docs := make([]Document, len(rows))
	for i, row := range rows {
		docs[i] = postgresDocumentToDomain(row)
	}
	return docs, nil
}

func (s *postgresStore) ListItemDocuments(ctx context.Context, itemID string) ([]Document, error) {
	rows, err := s.q.ListItemDocuments(ctx, nullString(&itemID))
	if err != nil {
		return nil, err
	}
	docs := make([]Document, len(rows))
	for i, row := range rows {
		docs[i] = postgresDocumentToDomain(row)
	}
	return docs, nil
}

func (s *postgresStore) DeleteDocument(ctx context.Context, id, tripID string) (bool, error) {
	n, err := s.q.DeleteDocument(ctx, postgresgen.DeleteDocumentParams{ID: id, TripID: tripID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *postgresStore) CreateChecklist(ctx context.Context, p CreateChecklistParams) (Checklist, error) {
	row, err := s.q.CreateChecklist(ctx, postgresgen.CreateChecklistParams{
		ID:        p.ID,
		TripID:    p.TripID,
		Title:     p.Title,
		SortOrder: int32(p.SortOrder),
		CreatedAt: p.CreatedAt.UTC(),
	})
	if err != nil {
		return Checklist{}, err
	}
	return postgresChecklistToDomain(row), nil
}

func (s *postgresStore) GetChecklistByID(ctx context.Context, id string) (Checklist, error) {
	row, err := s.q.GetChecklistByID(ctx, id)
	if err != nil {
		return Checklist{}, mapNotFound(err)
	}
	return postgresChecklistToDomain(row), nil
}

func (s *postgresStore) ListChecklistsByTrip(ctx context.Context, tripID string) ([]Checklist, error) {
	rows, err := s.q.ListChecklistsByTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	checklists := make([]Checklist, len(rows))
	for i, row := range rows {
		checklists[i] = postgresChecklistToDomain(row)
	}
	return checklists, nil
}

func (s *postgresStore) DeleteChecklist(ctx context.Context, id, tripID string) (bool, error) {
	n, err := s.q.DeleteChecklist(ctx, postgresgen.DeleteChecklistParams{ID: id, TripID: tripID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *postgresStore) CreateChecklistItem(ctx context.Context, p CreateChecklistItemParams) (ChecklistItem, error) {
	row, err := s.q.CreateChecklistItem(ctx, postgresgen.CreateChecklistItemParams{
		ID:          p.ID,
		ChecklistID: p.ChecklistID,
		Text:        p.Text,
		Checked:     p.Checked,
		SortOrder:   int32(p.SortOrder),
		CreatedAt:   p.CreatedAt.UTC(),
	})
	if err != nil {
		return ChecklistItem{}, err
	}
	return postgresChecklistItemToDomain(row), nil
}

func (s *postgresStore) ListChecklistItemsByChecklist(ctx context.Context, checklistID string) ([]ChecklistItem, error) {
	rows, err := s.q.ListChecklistItemsByChecklist(ctx, checklistID)
	if err != nil {
		return nil, err
	}
	items := make([]ChecklistItem, len(rows))
	for i, row := range rows {
		items[i] = postgresChecklistItemToDomain(row)
	}
	return items, nil
}

func (s *postgresStore) SetChecklistItemChecked(ctx context.Context, id, checklistID string, checked bool) (ChecklistItem, error) {
	row, err := s.q.SetChecklistItemChecked(ctx, postgresgen.SetChecklistItemCheckedParams{
		ID:          id,
		ChecklistID: checklistID,
		Checked:     checked,
	})
	if err != nil {
		return ChecklistItem{}, mapNotFound(err)
	}
	return postgresChecklistItemToDomain(row), nil
}

func (s *postgresStore) DeleteChecklistItem(ctx context.Context, id, checklistID string) (bool, error) {
	n, err := s.q.DeleteChecklistItem(ctx, postgresgen.DeleteChecklistItemParams{ID: id, ChecklistID: checklistID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func postgresChecklistToDomain(c postgresgen.Checklist) Checklist {
	return Checklist{
		ID:        c.ID,
		TripID:    c.TripID,
		Title:     c.Title,
		SortOrder: int(c.SortOrder),
		CreatedAt: c.CreatedAt,
	}
}

func postgresChecklistItemToDomain(c postgresgen.ChecklistItem) ChecklistItem {
	return ChecklistItem{
		ID:          c.ID,
		ChecklistID: c.ChecklistID,
		Text:        c.Text,
		Checked:     c.Checked,
		SortOrder:   int(c.SortOrder),
		CreatedAt:   c.CreatedAt,
	}
}

func postgresDocumentToDomain(d postgresgen.Document) Document {
	return Document{
		ID:          d.ID,
		TripID:      d.TripID,
		ItemID:      strPtr(d.ItemID),
		Filename:    d.Filename,
		StoragePath: d.StoragePath,
		ContentType: strPtr(d.ContentType),
		SizeBytes:   d.SizeBytes,
		UploadedAt:  d.UploadedAt,
		Note:        strPtr(d.Note),
	}
}

func (s *postgresStore) WithTx(ctx context.Context, fn func(Store) error) error {
	conn, ok := s.db.(*sql.DB)
	if !ok {
		return errors.New("postgresStore.WithTx: not backed by *sql.DB")
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	txStore := &postgresStore{q: s.q.WithTx(tx), db: tx}
	if err := fn(txStore); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func postgresUserToDomain(u postgresgen.User) User {
	return User{
		ID:          u.ID,
		Username:    u.Username,
		DisplayName: u.DisplayName,
		Email:       strPtr(u.Email),
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
	}
}

func postgresAuthIdentityToDomain(a postgresgen.AuthIdentity) AuthIdentity {
	return AuthIdentity{
		ID:             a.ID,
		UserID:         a.UserID,
		Provider:       a.Provider,
		ProviderUserID: a.ProviderUserID,
		PasswordHash:   strPtr(a.PasswordHash),
		CreatedAt:      a.CreatedAt,
	}
}

func postgresSessionToDomain(s postgresgen.Session) Session {
	return Session{
		ID:         s.ID,
		UserID:     s.UserID,
		CreatedAt:  s.CreatedAt,
		ExpiresAt:  s.ExpiresAt,
		LastSeenAt: s.LastSeenAt,
		UserAgent:  strPtr(s.UserAgent),
		IP:         strPtr(s.Ip),
	}
}
