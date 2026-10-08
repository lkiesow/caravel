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
		IsAdmin:     p.IsAdmin,
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

func (s *postgresStore) UpdateAuthIdentityPassword(ctx context.Context, provider, providerUserID, passwordHash string) error {
	return s.q.UpdateAuthIdentityPassword(ctx, postgresgen.UpdateAuthIdentityPasswordParams{
		PasswordHash:   nullString(&passwordHash),
		Provider:       provider,
		ProviderUserID: providerUserID,
	})
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

func (s *postgresStore) DeleteSessionsByUserID(ctx context.Context, userID string) error {
	return s.q.DeleteSessionsByUserID(ctx, userID)
}

func (s *postgresStore) DeleteExpiredSessions(ctx context.Context, now time.Time) error {
	return s.q.DeleteExpiredSessions(ctx, now.UTC())
}

func (s *postgresStore) CountActiveSessions(ctx context.Context, now time.Time) (int64, error) {
	return s.q.CountActiveSessions(ctx, now.UTC())
}

func (s *postgresStore) InstanceCounts(ctx context.Context) (InstanceCounts, error) {
	row, err := s.q.InstanceCounts(ctx)
	if err != nil {
		return InstanceCounts{}, err
	}
	cats, err := s.q.CountLocationsByCategory(ctx)
	if err != nil {
		return InstanceCounts{}, err
	}
	c := InstanceCounts{
		Users:               row.Users,
		Trips:               row.Trips,
		Files:               row.Files,
		FileBytes:           row.FileBytes,
		Expenses:            row.Expenses,
		LocationsByCategory: make(map[string]int64, len(cats)),
	}
	for _, r := range cats {
		c.LocationsByCategory[r.Category] = r.LocationCount
	}
	return c, nil
}

const dateLayout = "2006-01-02"

func (s *postgresStore) CreateLocation(ctx context.Context, p CreateLocationParams) (Location, error) {
	row, err := s.q.CreateLocation(ctx, postgresgen.CreateLocationParams{
		ID:        p.ID,
		TripID:    p.TripID,
		Category:  p.Category,
		Title:     p.Title,
		Notes:     nullString(p.Notes),
		ShowOnMap: p.ShowOnMap,
		CreatedAt: p.CreatedAt.UTC(),
		UpdatedAt: p.UpdatedAt.UTC(),
	})
	if err != nil {
		return Location{}, err
	}
	return postgresLocationToDomain(row), nil
}

func (s *postgresStore) GetLocationByID(ctx context.Context, id string) (Location, error) {
	row, err := s.q.GetLocationByID(ctx, id)
	if err != nil {
		return Location{}, mapNotFound(err)
	}
	return postgresLocationToDomain(row), nil
}

func (s *postgresStore) ListLocationsByTrip(ctx context.Context, tripID string, category *string) ([]Location, error) {
	rows, err := s.q.ListLocationsByTrip(ctx, postgresgen.ListLocationsByTripParams{
		TripID:   tripID,
		Category: nullString(category),
	})
	if err != nil {
		return nil, err
	}
	locations := make([]Location, len(rows))
	for i, row := range rows {
		locations[i] = postgresLocationToDomain(row)
	}
	return locations, nil
}

func (s *postgresStore) UpdateLocation(ctx context.Context, p UpdateLocationParams) (Location, error) {
	row, err := s.q.UpdateLocation(ctx, postgresgen.UpdateLocationParams{
		ID:        p.ID,
		TripID:    p.TripID,
		Category:  p.Category,
		Title:     p.Title,
		Notes:     nullString(p.Notes),
		ShowOnMap: p.ShowOnMap,
		UpdatedAt: p.UpdatedAt.UTC(),
	})
	if err != nil {
		return Location{}, mapNotFound(err)
	}
	return postgresLocationToDomain(row), nil
}

func (s *postgresStore) DeleteLocation(ctx context.Context, id, tripID string) (bool, error) {
	n, err := s.q.DeleteLocation(ctx, postgresgen.DeleteLocationParams{ID: id, TripID: tripID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *postgresStore) SetLocationImage(ctx context.Context, id, tripID string, imageID *string, updatedAt time.Time) (Location, error) {
	row, err := s.q.SetLocationImage(ctx, postgresgen.SetLocationImageParams{
		ID:        id,
		TripID:    tripID,
		ImageID:   nullString(imageID),
		UpdatedAt: updatedAt.UTC(),
	})
	if err != nil {
		return Location{}, mapNotFound(err)
	}
	return postgresLocationToDomain(row), nil
}

func (s *postgresStore) UpsertLocationGeo(ctx context.Context, p UpsertLocationGeoParams) (LocationGeo, error) {
	n, err := s.q.UpdateLocationGeo(ctx, postgresgen.UpdateLocationGeoParams{
		LocationID: p.LocationID,
		Lat:        nullFloat64(p.Lat),
		Lng:        nullFloat64(p.Lng),
		Address:    nullString(p.Address),
		OsmType:    nullString(p.OSMType),
		OsmID:      nullString(p.OSMID),
	})
	if err != nil {
		return LocationGeo{}, err
	}
	if n > 0 {
		return s.GetLocationGeoByLocationID(ctx, p.LocationID)
	}

	row, err := s.q.InsertLocationGeo(ctx, postgresgen.InsertLocationGeoParams{
		ID:         p.ID,
		LocationID: p.LocationID,
		Lat:        nullFloat64(p.Lat),
		Lng:        nullFloat64(p.Lng),
		Address:    nullString(p.Address),
		OsmType:    nullString(p.OSMType),
		OsmID:      nullString(p.OSMID),
	})
	if err != nil {
		return LocationGeo{}, err
	}
	return postgresLocationGeoToDomain(row), nil
}

func (s *postgresStore) GetLocationGeoByLocationID(ctx context.Context, locationID string) (LocationGeo, error) {
	row, err := s.q.GetLocationGeoByLocationID(ctx, locationID)
	if err != nil {
		return LocationGeo{}, mapNotFound(err)
	}
	return postgresLocationGeoToDomain(row), nil
}

func (s *postgresStore) CreateLocationLink(ctx context.Context, p CreateLocationLinkParams) (LocationLink, error) {
	row, err := s.q.CreateLocationLink(ctx, postgresgen.CreateLocationLinkParams{
		ID:         p.ID,
		LocationID: p.LocationID,
		Url:        p.URL,
		Label:      nullString(p.Label),
		SortOrder:  int32(p.SortOrder),
	})
	if err != nil {
		return LocationLink{}, err
	}
	return postgresLocationLinkToDomain(row), nil
}

func (s *postgresStore) ListLocationLinksByLocation(ctx context.Context, locationID string) ([]LocationLink, error) {
	rows, err := s.q.ListLocationLinksByLocation(ctx, locationID)
	if err != nil {
		return nil, err
	}
	links := make([]LocationLink, len(rows))
	for i, row := range rows {
		links[i] = postgresLocationLinkToDomain(row)
	}
	return links, nil
}

func (s *postgresStore) DeleteLocationLink(ctx context.Context, id, locationID string) (bool, error) {
	n, err := s.q.DeleteLocationLink(ctx, postgresgen.DeleteLocationLinkParams{ID: id, LocationID: locationID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *postgresStore) CreateLocationTag(ctx context.Context, locationID, tag string) error {
	return s.q.CreateLocationTag(ctx, postgresgen.CreateLocationTagParams{LocationID: locationID, Tag: tag})
}

func (s *postgresStore) ListLocationTagsByLocation(ctx context.Context, locationID string) ([]string, error) {
	tags, err := s.q.ListLocationTagsByLocation(ctx, locationID)
	if err != nil {
		return nil, err
	}
	// Never nil: the tag set is a JSON array on the wire, and a null there
	// would make "no tags" a different shape from "tags I removed".
	if tags == nil {
		tags = []string{}
	}
	return tags, nil
}

func (s *postgresStore) ListLocationTagsByTrip(ctx context.Context, tripID string) ([]LocationTag, error) {
	rows, err := s.q.ListLocationTagsByTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	tags := make([]LocationTag, len(rows))
	for i, row := range rows {
		tags[i] = LocationTag{LocationID: row.LocationID, Tag: row.Tag}
	}
	return tags, nil
}

func (s *postgresStore) DeleteLocationTagsByLocation(ctx context.Context, locationID string) error {
	return s.q.DeleteLocationTagsByLocation(ctx, locationID)
}

func postgresLocationToDomain(i postgresgen.Location) Location {
	return Location{
		ID:        i.ID,
		TripID:    i.TripID,
		Category:  i.Category,
		Title:     i.Title,
		Notes:     strPtr(i.Notes),
		ImageID:   strPtr(i.ImageID),
		ShowOnMap: i.ShowOnMap,
		CreatedAt: i.CreatedAt,
		UpdatedAt: i.UpdatedAt,
	}
}

func postgresLocationGeoToDomain(l postgresgen.LocationGeo) LocationGeo {
	return LocationGeo{
		ID:         l.ID,
		LocationID: l.LocationID,
		Lat:        floatPtr(l.Lat),
		Lng:        floatPtr(l.Lng),
		Address:    strPtr(l.Address),
		OSMType:    strPtr(l.OsmType),
		OSMID:      strPtr(l.OsmID),
	}
}

func postgresLocationLinkToDomain(l postgresgen.LocationLink) LocationLink {
	return LocationLink{
		ID:         l.ID,
		LocationID: l.LocationID,
		URL:        l.Url,
		Label:      strPtr(l.Label),
		SortOrder:  int(l.SortOrder),
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

func (s *postgresStore) CountUsers(ctx context.Context) (int64, error) {
	return s.q.CountUsers(ctx)
}

// SHARE ROW EXCLUSIVE conflicts with itself and with the ROW EXCLUSIVE lock
// every INSERT, UPDATE and DELETE on users takes, but not with plain reads or
// with the foreign-key checks of rows referencing a user. A table lock rather
// than an advisory one because the latter is per database, and the Postgres
// tests share one database with a schema each.
//
// Raw SQL because the queries are shared with SQLite, which has no LOCK.
func (s *postgresStore) LockUserCreation(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `LOCK TABLE users IN SHARE ROW EXCLUSIVE MODE`)
	return err
}

func (s *postgresStore) ListUsers(ctx context.Context) ([]UserWithTripCount, error) {
	rows, err := s.q.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	users := make([]UserWithTripCount, len(rows))
	for i, row := range rows {
		users[i] = UserWithTripCount{
			User: User{
				ID:          row.ID,
				Username:    row.Username,
				DisplayName: row.DisplayName,
				IsAdmin:     row.IsAdmin,
				CreatedAt:   row.CreatedAt,
			},
			TripCount: row.TripCount,
		}
	}
	return users, nil
}

func (s *postgresStore) CountAdmins(ctx context.Context) (int64, error) {
	return s.q.CountAdmins(ctx, true)
}

func (s *postgresStore) UpdateUser(ctx context.Context, p UpdateUserParams) (User, error) {
	row, err := s.q.UpdateUser(ctx, postgresgen.UpdateUserParams{
		ID:          p.ID,
		DisplayName: p.DisplayName,
		IsAdmin:     p.IsAdmin,
		UpdatedAt:   p.UpdatedAt.UTC(),
	})
	if err != nil {
		return User{}, mapNotFound(err)
	}
	return postgresUserToDomain(row), nil
}

func (s *postgresStore) DeleteUser(ctx context.Context, id string) (bool, error) {
	n, err := s.q.DeleteUser(ctx, id)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *postgresStore) GetAppSetting(ctx context.Context, name string) (string, error) {
	value, err := s.q.GetAppSetting(ctx, name)
	if err != nil {
		return "", mapNotFound(err)
	}
	return value, nil
}

func (s *postgresStore) SetAppSetting(ctx context.Context, name, value string) error {
	return s.q.SetAppSetting(ctx, postgresgen.SetAppSettingParams{Name: name, Value: value})
}

func (s *postgresStore) SearchUsers(ctx context.Context, query string, limit int) ([]UserSummary, error) {
	rows, err := s.q.SearchUsers(ctx, postgresgen.SearchUsersParams{
		Pattern: likeContains(query),
		// int32 here where sqlite's generated params say int64 — the two
		// dialects genuinely disagree, which is why the conversion lives at
		// each call site rather than in the shared signature.
		MaxResults: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]UserSummary, len(rows))
	for i, row := range rows {
		out[i] = UserSummary{ID: row.ID, Username: row.Username, DisplayName: row.DisplayName}
	}
	return out, nil
}

func (s *postgresStore) CreateTrip(ctx context.Context, p CreateTripParams) (Trip, error) {
	row, err := s.q.CreateTrip(ctx, postgresgen.CreateTripParams{
		ID:        p.ID,
		OwnerID:   p.OwnerID,
		Title:     p.Title,
		StartDate: nullDate(p.StartDate),
		EndDate:   nullDate(p.EndDate),
		Subtitle:  nullString(p.Subtitle),
		Currency:  p.Currency,
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

func (s *postgresStore) ListTripsForUser(ctx context.Context, userID string) ([]TripForUser, error) {
	rows, err := s.q.ListTripsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	trips := make([]TripForUser, len(rows))
	for i, row := range rows {
		trips[i] = TripForUser{
			Trip: Trip{
				ID:             row.ID,
				OwnerID:        row.OwnerID,
				Title:          row.Title,
				StartDate:      datePtr(row.StartDate),
				EndDate:        datePtr(row.EndDate),
				PreviewImageID: strPtr(row.PreviewImageID),
				Subtitle:       strPtr(row.Subtitle),
				Currency:       row.Currency,
				CreatedAt:      row.CreatedAt,
				UpdatedAt:      row.UpdatedAt,
			},
			Role:             TripRole(row.Role),
			OwnerUsername:    row.OwnerUsername,
			OwnerDisplayName: row.OwnerDisplayName,
			MemberCount:      row.MemberCount,
		}
	}
	return trips, nil
}

func (s *postgresStore) UpdateTrip(ctx context.Context, p UpdateTripParams) (Trip, error) {
	row, err := s.q.UpdateTrip(ctx, postgresgen.UpdateTripParams{
		ID:        p.ID,
		Title:     p.Title,
		StartDate: nullDate(p.StartDate),
		EndDate:   nullDate(p.EndDate),
		Subtitle:  nullString(p.Subtitle),
		Currency:  p.Currency,
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

func (s *postgresStore) SetTripPreviewImage(ctx context.Context, id string, imageID *string, updatedAt time.Time) (Trip, error) {
	row, err := s.q.SetTripPreviewImage(ctx, postgresgen.SetTripPreviewImageParams{
		ID:             id,
		PreviewImageID: nullString(imageID),
		UpdatedAt:      updatedAt.UTC(),
	})
	if err != nil {
		return Trip{}, mapNotFound(err)
	}
	return postgresTripToDomain(row), nil
}

func (s *postgresStore) GetTripMember(ctx context.Context, tripID, userID string) (TripMember, error) {
	row, err := s.q.GetTripMember(ctx, postgresgen.GetTripMemberParams{TripID: tripID, UserID: userID})
	if err != nil {
		return TripMember{}, mapNotFound(err)
	}
	// GetTripMember does not join users, so the display fields stay empty here.
	// Callers that need them are listing people, and use ListTripMembers.
	return TripMember{
		TripID:    row.TripID,
		UserID:    row.UserID,
		Role:      TripRole(row.Role),
		CreatedAt: row.CreatedAt,
	}, nil
}

func (s *postgresStore) ListTripMembers(ctx context.Context, tripID string) ([]TripMember, error) {
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
			CreatedAt:   row.CreatedAt,
			Username:    row.Username,
			DisplayName: row.DisplayName,
		}
	}
	return members, nil
}

func (s *postgresStore) UpsertTripMember(ctx context.Context, tripID, userID string, role TripRole, createdAt time.Time) (TripMember, error) {
	row, err := s.q.UpsertTripMember(ctx, postgresgen.UpsertTripMemberParams{
		TripID:    tripID,
		UserID:    userID,
		Role:      string(role),
		CreatedAt: createdAt.UTC(),
	})
	if err != nil {
		return TripMember{}, err
	}
	return TripMember{
		TripID:    row.TripID,
		UserID:    row.UserID,
		Role:      TripRole(row.Role),
		CreatedAt: row.CreatedAt,
	}, nil
}

func (s *postgresStore) DeleteTripMember(ctx context.Context, tripID, userID string) (bool, error) {
	n, err := s.q.DeleteTripMember(ctx, postgresgen.DeleteTripMemberParams{TripID: tripID, UserID: userID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *postgresStore) CountTripMembers(ctx context.Context, tripID string) (int64, error) {
	return s.q.CountTripMembers(ctx, tripID)
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
		SourceUrl:   nullString(p.SourceURL),
		Credit:      nullString(p.Credit),
		License:     nullString(p.License),
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
		SourceURL:   strPtr(m.SourceUrl),
		Credit:      strPtr(m.Credit),
		License:     strPtr(m.License),
		Width:       intPtr32(m.Width),
		Height:      intPtr32(m.Height),
		CreatedAt:   m.CreatedAt,
	}
}

func (s *postgresStore) CreateExpense(ctx context.Context, p CreateExpenseParams) (Expense, error) {
	spentOn, err := time.Parse(dateLayout, p.SpentOn)
	if err != nil {
		return Expense{}, err
	}
	row, err := s.q.CreateExpense(ctx, postgresgen.CreateExpenseParams{
		ID:          p.ID,
		TripID:      p.TripID,
		Title:       p.Title,
		AmountMinor: p.AmountMinor,
		Currency:    nullString(p.Currency),
		SpentOn:     spentOn,
		PayerUserID: nullString(p.PayerUserID),
		LocationID:  nullString(p.LocationID),
		CreatedAt:   p.CreatedAt.UTC(),
	})
	if err != nil {
		return Expense{}, err
	}
	return postgresExpenseToDomain(row), nil
}

func (s *postgresStore) GetExpenseByID(ctx context.Context, id string) (Expense, error) {
	row, err := s.q.GetExpenseByID(ctx, id)
	if err != nil {
		return Expense{}, mapNotFound(err)
	}
	return postgresExpenseToDomain(row), nil
}

func (s *postgresStore) ListExpensesByTrip(ctx context.Context, tripID string) ([]Expense, error) {
	rows, err := s.q.ListExpensesByTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	expenses := make([]Expense, len(rows))
	for i, row := range rows {
		expenses[i] = postgresExpenseToDomain(row)
	}
	return expenses, nil
}

func (s *postgresStore) UpdateExpense(ctx context.Context, p UpdateExpenseParams) (Expense, error) {
	spentOn, err := time.Parse(dateLayout, p.SpentOn)
	if err != nil {
		return Expense{}, err
	}
	row, err := s.q.UpdateExpense(ctx, postgresgen.UpdateExpenseParams{
		ID:          p.ID,
		TripID:      p.TripID,
		Title:       p.Title,
		AmountMinor: p.AmountMinor,
		Currency:    nullString(p.Currency),
		SpentOn:     spentOn,
		PayerUserID: nullString(p.PayerUserID),
		LocationID:  nullString(p.LocationID),
	})
	if err != nil {
		return Expense{}, mapNotFound(err)
	}
	return postgresExpenseToDomain(row), nil
}

func (s *postgresStore) DeleteExpense(ctx context.Context, id, tripID string) (bool, error) {
	n, err := s.q.DeleteExpense(ctx, postgresgen.DeleteExpenseParams{ID: id, TripID: tripID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// spent_on is a real DATE here and TEXT in sqlite, so this is where the two
// dialects are brought back to the same "YYYY-MM-DD" string the domain type
// promises. Same treatment as postgresItineraryDayToDomain.
func (s *postgresStore) CreateExpenseShare(ctx context.Context, expenseID, userID string) error {
	return s.q.CreateExpenseShare(ctx, postgresgen.CreateExpenseShareParams{ExpenseID: expenseID, UserID: userID})
}

func (s *postgresStore) DeleteExpenseSharesByExpense(ctx context.Context, expenseID string) error {
	return s.q.DeleteExpenseSharesByExpense(ctx, expenseID)
}

func (s *postgresStore) ListExpenseShareUsers(ctx context.Context, expenseID string) ([]string, error) {
	return s.q.ListExpenseSharesByExpense(ctx, expenseID)
}

func (s *postgresStore) ListExpenseSharesByTrip(ctx context.Context, tripID string) ([]ExpenseShare, error) {
	rows, err := s.q.ListExpenseSharesByTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	shares := make([]ExpenseShare, len(rows))
	for i, row := range rows {
		shares[i] = ExpenseShare{ExpenseID: row.ExpenseID, UserID: row.UserID}
	}
	return shares, nil
}

func (s *postgresStore) ListTripCurrencies(ctx context.Context, tripID string) ([]TripCurrency, error) {
	rows, err := s.q.ListTripCurrencies(ctx, tripID)
	if err != nil {
		return nil, err
	}
	currencies := make([]TripCurrency, len(rows))
	for i, row := range rows {
		currencies[i] = postgresTripCurrencyToDomain(row)
	}
	return currencies, nil
}

func (s *postgresStore) CreateTripCurrency(ctx context.Context, p CreateTripCurrencyParams) (TripCurrency, error) {
	row, err := s.q.CreateTripCurrency(ctx, postgresgen.CreateTripCurrencyParams{
		TripID:    p.TripID,
		Code:      p.Code,
		RatePpb:   p.RatePPB,
		CreatedAt: p.CreatedAt.UTC(),
	})
	if err != nil {
		return TripCurrency{}, err
	}
	return postgresTripCurrencyToDomain(row), nil
}

func (s *postgresStore) DeleteTripCurrenciesByTrip(ctx context.Context, tripID string) error {
	return s.q.DeleteTripCurrenciesByTrip(ctx, tripID)
}

func (s *postgresStore) CountExpensesByCurrency(ctx context.Context, tripID string) ([]CurrencyUsage, error) {
	rows, err := s.q.CountExpensesByCurrency(ctx, tripID)
	if err != nil {
		return nil, err
	}
	usage := make([]CurrencyUsage, 0, len(rows))
	for _, row := range rows {
		// The query already excludes NULL; see the SQLite twin.
		if !row.Currency.Valid {
			continue
		}
		usage = append(usage, CurrencyUsage{Code: row.Currency.String, ExpenseCount: row.ExpenseCount})
	}
	return usage, nil
}

func postgresTripCurrencyToDomain(c postgresgen.TripCurrency) TripCurrency {
	return TripCurrency{
		TripID:    c.TripID,
		Code:      c.Code,
		RatePPB:   c.RatePpb,
		CreatedAt: c.CreatedAt,
	}
}

func postgresExpenseToDomain(e postgresgen.Expense) Expense {
	return Expense{
		ID:          e.ID,
		TripID:      e.TripID,
		Title:       e.Title,
		AmountMinor: e.AmountMinor,
		Currency:    strPtr(e.Currency),
		SpentOn:     e.SpentOn.Format(dateLayout),
		PayerUserID: strPtr(e.PayerUserID),
		LocationID:  strPtr(e.LocationID),
		CreatedAt:   e.CreatedAt,
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
		Currency:       t.Currency,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
	}
}

func (s *postgresStore) ListMapLocations(ctx context.Context, tripID string) ([]MapLocation, error) {
	rows, err := s.q.ListMapLocationsByTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	locations := make([]MapLocation, 0, len(rows))
	for _, row := range rows {
		if !row.ShowOnMap {
			continue
		}
		locations = append(locations, MapLocation{
			ID:       row.ID,
			Category: row.Category,
			Title:    row.Title,
			Lat:      row.Lat.Float64,
			Lng:      row.Lng.Float64,
			Address:  strPtr(row.Address),
			ImageID:  strPtr(row.ImageID),
		})
	}
	return locations, nil
}

func (s *postgresStore) ListLocationCoordinates(ctx context.Context, tripID string) ([]LocationCoordinate, error) {
	rows, err := s.q.ListLocationCoordinatesByTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	out := make([]LocationCoordinate, 0, len(rows))
	for _, row := range rows {
		// The query already excludes NULL lat/lng, but the generated types are
		// still nullable because the columns are; a row that somehow arrived
		// without both is dropped rather than reported at 0,0.
		if !row.Lat.Valid || !row.Lng.Valid {
			continue
		}
		out = append(out, LocationCoordinate{LocationID: row.LocationID, Lat: row.Lat.Float64, Lng: row.Lng.Float64})
	}
	return out, nil
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

func (s *postgresStore) EnsureItineraryDay(ctx context.Context, newID, tripID, date string) (ItineraryDay, error) {
	parsedDate, err := time.Parse(dateLayout, date)
	if err != nil {
		return ItineraryDay{}, err
	}

	// A conflicting insert from a transaction that has not committed yet
	// waits for it, and the read that follows sees its row: each statement
	// takes a fresh snapshot under READ COMMITTED.
	err = s.q.InsertItineraryDayIfAbsent(ctx, postgresgen.InsertItineraryDayIfAbsentParams{
		ID:     newID,
		TripID: tripID,
		Date:   parsedDate,
	})
	if err != nil {
		return ItineraryDay{}, err
	}
	row, err := s.q.GetItineraryDayByTripAndDate(ctx, postgresgen.GetItineraryDayByTripAndDateParams{TripID: tripID, Date: parsedDate})
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
		LocationID:     p.LocationID,
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
				LocationID:     row.LocationID,
				SortOrder:      int(row.SortOrder),
				Note:           strPtr(row.Note),
			},
			LocationTitle:    row.LocationTitle,
			LocationCategory: row.LocationCategory,
			LocationImageID:  strPtr(row.LocationImageID),
		}
	}
	return entries, nil
}

func (s *postgresStore) ListItineraryEntriesByDay(ctx context.Context, itineraryDayID string) ([]ItineraryEntry, error) {
	rows, err := s.q.ListItineraryEntriesByDay(ctx, itineraryDayID)
	if err != nil {
		return nil, err
	}
	entries := make([]ItineraryEntry, len(rows))
	for i, row := range rows {
		entries[i] = postgresItineraryEntryToDomain(row)
	}
	return entries, nil
}

func (s *postgresStore) SetItineraryEntrySortOrder(ctx context.Context, id, itineraryDayID string, sortOrder int) (bool, error) {
	n, err := s.q.SetItineraryEntrySortOrder(ctx, postgresgen.SetItineraryEntrySortOrderParams{
		ID:             id,
		ItineraryDayID: itineraryDayID,
		// int32 here where sqlite takes int64: the two generated packages differ
		// on integer width, which is exactly the sort of thing only a build
		// catches today (see the postgres note in todo.md).
		SortOrder: int32(sortOrder),
	})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *postgresStore) SetItineraryEntryDay(ctx context.Context, id, fromDayID, toDayID string, sortOrder int) (bool, error) {
	n, err := s.q.SetItineraryEntryDay(ctx, postgresgen.SetItineraryEntryDayParams{
		ID:                 id,
		FromItineraryDayID: fromDayID,
		ToItineraryDayID:   toDayID,
		// int32 here where sqlite takes int64, the same dialect difference
		// SetItineraryEntrySortOrder notes above.
		SortOrder: int32(sortOrder),
	})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *postgresStore) DeleteItineraryEntry(ctx context.Context, id, itineraryDayID string) (bool, error) {
	n, err := s.q.DeleteItineraryEntry(ctx, postgresgen.DeleteItineraryEntryParams{ID: id, ItineraryDayID: itineraryDayID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// The date comes back as a time.Time on this dialect, where itinerary_days.date
// is a DATE rather than the TEXT it is on SQLite, so it is formatted back to
// the "YYYY-MM-DD" the rest of the application passes around — exactly what
// postgresItineraryDayToDomain below does with the same column.
func (s *postgresStore) ListItineraryDatesByLocation(ctx context.Context, locationID string) ([]LocationItineraryDate, error) {
	rows, err := s.q.ListItineraryDatesByLocation(ctx, locationID)
	if err != nil {
		return nil, err
	}
	dates := make([]LocationItineraryDate, len(rows))
	for i, row := range rows {
		dates[i] = LocationItineraryDate{
			LocationID: row.LocationID,
			EntryID:    row.EntryID,
			DayID:      row.DayID,
			Date:       row.Date.Format(dateLayout),
			SortOrder:  int(row.SortOrder),
		}
	}
	return dates, nil
}

func (s *postgresStore) ListLocationDatesByTrip(ctx context.Context, tripID string) ([]LocationItineraryDate, error) {
	rows, err := s.q.ListLocationDatesByTrip(ctx, tripID)
	if err != nil {
		return nil, err
	}
	dates := make([]LocationItineraryDate, len(rows))
	for i, row := range rows {
		dates[i] = LocationItineraryDate{
			LocationID: row.LocationID,
			EntryID:    row.EntryID,
			DayID:      row.DayID,
			Date:       row.Date.Format(dateLayout),
			SortOrder:  int(row.SortOrder),
		}
	}
	return dates, nil
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
		LocationID:     e.LocationID,
		SortOrder:      int(e.SortOrder),
		Note:           strPtr(e.Note),
	}
}

func (s *postgresStore) CreateFile(ctx context.Context, p CreateFileParams) (File, error) {
	row, err := s.q.CreateFile(ctx, postgresgen.CreateFileParams{
		ID:          p.ID,
		TripID:      p.TripID,
		LocationID:  nullString(p.LocationID),
		Filename:    p.Filename,
		StoragePath: p.StoragePath,
		ContentType: nullString(p.ContentType),
		SizeBytes:   p.SizeBytes,
		UploadedAt:  p.UploadedAt.UTC(),
		Note:        nullString(p.Note),
		Visibility:  string(p.Visibility),
		OwnerUserID: nullString(p.OwnerUserID),
	})
	if err != nil {
		return File{}, err
	}
	return postgresFileToDomain(row), nil
}

func (s *postgresStore) GetFileByID(ctx context.Context, id string) (File, error) {
	row, err := s.q.GetFileByID(ctx, id)
	if err != nil {
		return File{}, mapNotFound(err)
	}
	return postgresFileToDomain(row), nil
}

func (s *postgresStore) ListTripFiles(ctx context.Context, tripID, userID string) ([]FileDetail, error) {
	rows, err := s.q.ListTripFiles(ctx, postgresgen.ListTripFilesParams{TripID: tripID, UserID: nullString(&userID)})
	if err != nil {
		return nil, err
	}
	files := make([]FileDetail, len(rows))
	for i, row := range rows {
		files[i] = FileDetail{
			// The joined row is its own generated struct, so this can't go
			// through postgresFileToDomain like the other file queries.
			File: File{
				ID:          row.ID,
				TripID:      row.TripID,
				LocationID:  strPtr(row.LocationID),
				Filename:    row.Filename,
				StoragePath: row.StoragePath,
				ContentType: strPtr(row.ContentType),
				SizeBytes:   row.SizeBytes,
				UploadedAt:  row.UploadedAt,
				Note:        strPtr(row.Note),
				Visibility:  FileVisibility(row.Visibility),
				OwnerUserID: strPtr(row.OwnerUserID),
			},
			LocationTitle: strPtr(row.LocationTitle),
		}
	}
	return files, nil
}

func (s *postgresStore) ListLocationFiles(ctx context.Context, locationID, userID string) ([]File, error) {
	rows, err := s.q.ListLocationFiles(ctx, postgresgen.ListLocationFilesParams{LocationID: nullString(&locationID), UserID: nullString(&userID)})
	if err != nil {
		return nil, err
	}
	files := make([]File, len(rows))
	for i, row := range rows {
		files[i] = postgresFileToDomain(row)
	}
	return files, nil
}

func (s *postgresStore) UpdateFileNote(ctx context.Context, id, tripID string, note *string) (File, error) {
	row, err := s.q.UpdateFileNote(ctx, postgresgen.UpdateFileNoteParams{
		Note:   nullString(note),
		ID:     id,
		TripID: tripID,
	})
	if err != nil {
		return File{}, mapNotFound(err)
	}
	return postgresFileToDomain(row), nil
}

func (s *postgresStore) SetFileVisibility(ctx context.Context, id, tripID string, visibility FileVisibility) (File, error) {
	row, err := s.q.SetFileVisibility(ctx, postgresgen.SetFileVisibilityParams{
		Visibility: string(visibility),
		ID:         id,
		TripID:     tripID,
	})
	if err != nil {
		return File{}, mapNotFound(err)
	}
	return postgresFileToDomain(row), nil
}

func (s *postgresStore) ListPersonalFilesForUser(ctx context.Context, tripID, userID string) ([]File, error) {
	rows, err := s.q.ListPersonalFilesForUser(ctx, postgresgen.ListPersonalFilesForUserParams{
		TripID: tripID,
		UserID: nullString(&userID),
	})
	if err != nil {
		return nil, err
	}
	files := make([]File, len(rows))
	for i, row := range rows {
		files[i] = postgresFileToDomain(row)
	}
	return files, nil
}

func (s *postgresStore) DeleteFile(ctx context.Context, id, tripID string) (bool, error) {
	n, err := s.q.DeleteFile(ctx, postgresgen.DeleteFileParams{ID: id, TripID: tripID})
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (s *postgresStore) CreateChecklist(ctx context.Context, p CreateChecklistParams) (Checklist, error) {
	row, err := s.q.CreateChecklist(ctx, postgresgen.CreateChecklistParams{
		ID:          p.ID,
		TripID:      p.TripID,
		Title:       p.Title,
		SortOrder:   int32(p.SortOrder),
		Visibility:  string(p.Visibility),
		OwnerUserID: nullString(p.OwnerUserID),
		CreatedAt:   p.CreatedAt.UTC(),
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

func (s *postgresStore) ListChecklistsByTrip(ctx context.Context, tripID, userID string) ([]Checklist, error) {
	rows, err := s.q.ListChecklistsByTrip(ctx, postgresgen.ListChecklistsByTripParams{TripID: tripID, UserID: nullString(&userID)})
	if err != nil {
		return nil, err
	}
	checklists := make([]Checklist, len(rows))
	for i, row := range rows {
		checklists[i] = postgresChecklistToDomain(row)
	}
	return checklists, nil
}

func (s *postgresStore) SetChecklistVisibility(ctx context.Context, id, tripID string, visibility ChecklistVisibility) (Checklist, error) {
	row, err := s.q.SetChecklistVisibility(ctx, postgresgen.SetChecklistVisibilityParams{
		Visibility: string(visibility),
		ID:         id,
		TripID:     tripID,
	})
	if err != nil {
		return Checklist{}, mapNotFound(err)
	}
	return postgresChecklistToDomain(row), nil
}

func (s *postgresStore) UpdateChecklistTitle(ctx context.Context, id, tripID, title string) (Checklist, error) {
	row, err := s.q.UpdateChecklistTitle(ctx, postgresgen.UpdateChecklistTitleParams{
		Title:  title,
		ID:     id,
		TripID: tripID,
	})
	if err != nil {
		return Checklist{}, mapNotFound(err)
	}
	return postgresChecklistToDomain(row), nil
}

func (s *postgresStore) ListPersonalChecklistsForUser(ctx context.Context, tripID, userID string) ([]Checklist, error) {
	rows, err := s.q.ListPersonalChecklistsForUser(ctx, postgresgen.ListPersonalChecklistsForUserParams{
		TripID: tripID,
		UserID: nullString(&userID),
	})
	if err != nil {
		return nil, err
	}
	out := make([]Checklist, len(rows))
	for i, row := range rows {
		out[i] = postgresChecklistToDomain(row)
	}
	return out, nil
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

func (s *postgresStore) UpdateChecklistItemText(ctx context.Context, id, checklistID, text string) (ChecklistItem, error) {
	row, err := s.q.UpdateChecklistItemText(ctx, postgresgen.UpdateChecklistItemTextParams{
		Text:        text,
		ID:          id,
		ChecklistID: checklistID,
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
		ID:          c.ID,
		TripID:      c.TripID,
		Title:       c.Title,
		SortOrder:   int(c.SortOrder),
		CreatedAt:   c.CreatedAt,
		Visibility:  ChecklistVisibility(c.Visibility),
		OwnerUserID: strPtr(c.OwnerUserID),
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

// The trip notepad. Update-then-insert for the same reason the SQLite store
// does it that way; see the comment there.
func (s *postgresStore) GetTripNote(ctx context.Context, tripID string) (TripNote, error) {
	row, err := s.q.GetTripNote(ctx, tripID)
	if err != nil {
		return TripNote{}, mapNotFound(err)
	}
	return postgresTripNoteToDomain(row), nil
}

func (s *postgresStore) UpsertTripNote(ctx context.Context, p UpsertTripNoteParams) (TripNote, error) {
	n, err := s.q.UpdateTripNote(ctx, postgresgen.UpdateTripNoteParams{
		Body:      p.Body,
		UpdatedAt: p.UpdatedAt,
		UpdatedBy: nullString(p.UpdatedBy),
		TripID:    p.TripID,
	})
	if err != nil {
		return TripNote{}, err
	}
	if n > 0 {
		row, err := s.q.GetTripNote(ctx, p.TripID)
		if err != nil {
			return TripNote{}, err
		}
		return postgresTripNoteToDomain(row), nil
	}
	row, err := s.q.InsertTripNote(ctx, postgresgen.InsertTripNoteParams{
		TripID:    p.TripID,
		Body:      p.Body,
		UpdatedAt: p.UpdatedAt,
		UpdatedBy: nullString(p.UpdatedBy),
	})
	if err != nil {
		return TripNote{}, err
	}
	return postgresTripNoteToDomain(row), nil
}

func (s *postgresStore) DeleteTripNote(ctx context.Context, tripID string) (bool, error) {
	n, err := s.q.DeleteTripNote(ctx, tripID)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func postgresTripNoteToDomain(n postgresgen.TripNote) TripNote {
	return TripNote{
		TripID:    n.TripID,
		Body:      n.Body,
		UpdatedAt: n.UpdatedAt,
		UpdatedBy: strPtr(n.UpdatedBy),
	}
}

func postgresFileToDomain(d postgresgen.File) File {
	return File{
		ID:          d.ID,
		TripID:      d.TripID,
		LocationID:  strPtr(d.LocationID),
		Filename:    d.Filename,
		StoragePath: d.StoragePath,
		ContentType: strPtr(d.ContentType),
		SizeBytes:   d.SizeBytes,
		UploadedAt:  d.UploadedAt,
		Note:        strPtr(d.Note),
		Visibility:  FileVisibility(d.Visibility),
		OwnerUserID: strPtr(d.OwnerUserID),
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
		IsAdmin:     u.IsAdmin,
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
