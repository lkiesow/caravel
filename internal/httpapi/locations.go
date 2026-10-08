package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"caravel/internal/db"
	"caravel/internal/tags"
)

var validCategories = map[string]bool{
	"site": true, "stay": true, "transport": true, "area": true,
	"food": true, "event": true, "shop": true,
}

type locationResponse struct {
	ID        string  `json:"id"`
	TripID    string  `json:"trip_id"`
	Category  string  `json:"category"`
	Title     string  `json:"title"`
	Notes     *string `json:"notes"`
	NotesHTML *string `json:"notes_html"`
	ImageID   *string `json:"image_id"`
	ImageURL  *string `json:"image_url"`
	// ImageCredit is who the cover is owed to, or null -- which it is for
	// every image somebody uploaded themselves. Carried on the location rather
	// than only on the media asset because this is where it gets rendered,
	// and a second request to find out whether a credit exists would mean the
	// page either flickers or waits.
	ImageCredit *imageCreditResponse `json:"image_credit"`
	ShowOnMap   bool                 `json:"show_on_map"`
	// Lat/Lng are set only on the list endpoint, and only for locations that
	// have both. The list used to carry no position at all, which meant the
	// locations tab could not filter by distance without a second request
	// (Stage 13 Milestone 7). Flat rather than a nested "location" object
	// because there is no address here - the detail endpoint remains the place
	// to get a whole location.
	Lat *float64 `json:"lat,omitempty"`
	Lng *float64 `json:"lng,omitempty"`
	// Tags is always present and never null, on the list as well as the
	// detail: the locations tab filters on it client-side, and a field that
	// is sometimes absent would mean every caller writing the same guard.
	// locationToResponse leaves it empty and both handlers fill it in -- the
	// list from one trip-wide query, the detail from its own read.
	Tags []string `json:"tags"`
	// Dates is the itinerary days this location is on, collapsed into ranges.
	// On the list as well as the detail since Stage 26 Milestone 3, so the
	// cards can show them and the tab can filter and sort on them without a
	// request per card -- the same reasoning that put lat/lng here in Stage 13.
	// Always present, never null.
	Dates     []locationDateRangeResponse `json:"dates"`
	CreatedAt string                      `json:"created_at"`
	UpdatedAt string                      `json:"updated_at"`
}

func (s *Server) locationToResponse(ctx context.Context, i db.Location) locationResponse {
	resp := locationResponse{
		ID:        i.ID,
		TripID:    i.TripID,
		Category:  i.Category,
		Title:     i.Title,
		Notes:     i.Notes,
		NotesHTML: renderNotesHTML(i.Notes),
		ImageID:   i.ImageID,
		ShowOnMap: i.ShowOnMap,
		Tags:      []string{},
		Dates:     []locationDateRangeResponse{},
		CreatedAt: i.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: i.UpdatedAt.UTC().Format(time.RFC3339),
	}
	resp.ImageURL, resp.ImageCredit = s.resolveImage(ctx, i.ImageID)
	return resp
}

type geoResponse struct {
	Lat     *float64 `json:"lat"`
	Lng     *float64 `json:"lng"`
	Address *string  `json:"address"`
	// The OpenStreetMap element this place was saved from, when it was saved
	// through the address search. Null otherwise, which is the common case:
	// a dropped pin is not an OSM feature. The client renders a link to the
	// feature page when both are present.
	OSMType *string `json:"osm_type"`
	OSMID   *string `json:"osm_id"`
}

func newGeoResponse(geo db.LocationGeo) geoResponse {
	return geoResponse{
		Lat:     geo.Lat,
		Lng:     geo.Lng,
		Address: geo.Address,
		OSMType: geo.OSMType,
		OSMID:   geo.OSMID,
	}
}

type locationLinkResponse struct {
	ID        string  `json:"id"`
	URL       string  `json:"url"`
	Label     *string `json:"label"`
	SortOrder int     `json:"sort_order"`
}

type locationDetailResponse struct {
	locationResponse
	Geo   *geoResponse           `json:"geo"`
	Links []locationLinkResponse `json:"links"`
}

func (s *Server) handleListLocations(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleViewer)
	if !ok {
		return
	}

	var category *string
	if c := r.URL.Query().Get("category"); c != "" {
		if !validCategories[c] {
			writeError(w, http.StatusBadRequest, "invalid category filter")
			return
		}
		category = &c
	}

	locations, err := s.Store.ListLocationsByTrip(r.Context(), trip.ID, category)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list items")
		return
	}

	// One extra query for the whole trip rather than one per location. A
	// failure here costs the distance filter, not the list, so it is not fatal:
	// the tab still renders every location, just without coordinates to
	// measure.
	coordinates := map[string]db.LocationCoordinate{}
	if located, err := s.Store.ListLocationCoordinates(r.Context(), trip.ID); err == nil {
		for _, c := range located {
			coordinates[c.LocationID] = c
		}
	}

	// Likewise one query for the trip rather than one per location, and
	// likewise not fatal: a failure here costs the tag filter, not the list.
	tags := map[string][]string{}
	if rows, err := s.Store.ListLocationTagsByTrip(r.Context(), trip.ID); err == nil {
		tags = tagsByLocation(rows)
	}

	// And once more for the dates. Three trip-wide reads to build this list,
	// none of them per row, and none of them fatal -- see the note above.
	dates := map[string][]string{}
	if rows, err := s.Store.ListLocationDatesByTrip(r.Context(), trip.ID); err == nil {
		for _, row := range rows {
			dates[row.LocationID] = append(dates[row.LocationID], row.Date)
		}
	}

	resp := make([]locationResponse, len(locations))
	for i, it := range locations {
		resp[i] = s.locationToResponse(r.Context(), it)
		if c, ok := coordinates[it.ID]; ok {
			lat, lng := c.Lat, c.Lng
			resp[i].Lat, resp[i].Lng = &lat, &lng
		}
		if t, ok := tags[it.ID]; ok {
			resp[i].Tags = t
		}
		if d, ok := dates[it.ID]; ok {
			resp[i].Dates = collapseDateRanges(d)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

type locationRequest struct {
	Category  string  `json:"category"`
	Title     string  `json:"title"`
	Notes     *string `json:"notes"`
	ShowOnMap *bool   `json:"show_on_map"`

	// Optional nested sub-resources, so one request can commit a location and
	// everything hanging off it in a single transaction. Each is a pointer
	// so "absent" and "present but empty" stay distinguishable: absent
	// leaves that sub-resource untouched, present replaces it (an empty
	// list clears it). The standalone /location and /links endpoints still
	// exist and still work; these are additive.
	//
	// Geo is an upsert (location_geo.location_id is UNIQUE). Links are
	// replace-the-set rather than merge, because there is no per-row update
	// endpoint anywhere — editing a link has always meant delete plus re-add
	// — so the client edits them as a list and sends the list it wants. Array
	// order becomes sort_order for links.
	//
	// Dates are the exception, and the difference matters. Since Stage 25 they
	// are not rows of their own but a view of the itinerary days this location
	// appears on, so "present replaces it" is honoured by reconciling the day
	// set — see reconcileLocationDates — rather than by deleting and
	// recreating. The consequence for callers is that sending this key asserts
	// the location complete itinerary membership: a client that did not touch
	// the dates should omit it, not echo back what it read.
	Geo   *geoRequest                 `json:"geo"`
	Links *[]locationLinkRequest      `json:"links"`
	Dates *[]locationDateRangeRequest `json:"dates"`
	Tags  *[]string                   `json:"tags"`
}

func (req locationRequest) validate() error {
	if strings.TrimSpace(req.Title) == "" {
		return errors.New("title is required")
	}
	if !validCategories[req.Category] {
		return errors.New("category must be one of: site, stay, transport, area, food, event, shop")
	}
	// Validate the nested blocks up front so a bad link or date is a 400
	// before anything is written, rather than a rolled-back 500.
	if req.Geo != nil {
		if err := req.Geo.validate(); err != nil {
			return err
		}
	}
	if req.Links != nil {
		for _, l := range *req.Links {
			if err := validateLinkURL(l.URL); err != nil {
				return err
			}
		}
	}
	if req.Dates != nil {
		if err := validateLocationDateRanges(*req.Dates); err != nil {
			return err
		}
	}
	// Normalized before validating, so a set that is only over the limit
	// because it repeats a tag or pads one with spaces is accepted and
	// cleaned rather than refused.
	if req.Tags != nil {
		if err := tags.Validate(tags.Normalize(*req.Tags)); err != nil {
			return err
		}
	}
	return nil
}

// validateLinkURL accepts only what may safely become an href.
//
// This is a security check, not a tidiness one. A link is rendered by the
// client as <a href="...">, and until Stage 27 the only rule was that the URL
// was non-empty -- so "javascript:alert(1)" was stored happily and rendered as
// a working link. On a shared trip that is stored XSS rather than a way to
// attack yourself: any editor can plant it, and any member who opens that
// location and clicks runs the script with their own session.
//
// http and https only. Deliberately not mailto, tel or anything else that is
// individually harmless: the field is presented as a web link, the assistant
// only ever proposes addresses it has fetched, and every scheme added here is
// a scheme every current and future render site has to be safe for. Widening
// it later is one line and a test; narrowing it after someone has stored a
// thousand of them is not.
//
// The guard lives here rather than only at the render sites because it is the
// boundary every client shares -- but note that both render sites check as
// well, since a link stored before this existed is still in the database.
func validateLinkURL(raw string) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return errors.New("every link needs a url")
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return errors.New("a link must be a valid http or https url")
	}
	// Lowercased because a scheme is case-insensitive and "JavaScript:" is the
	// obvious next attempt.
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return errors.New("a link must be an http or https url")
	}
	// A scheme with no host is "https:" followed by whatever the browser makes
	// of the rest, which is not a link to anywhere.
	if u.Host == "" {
		return errors.New("a link must include a host")
	}
	return nil
}

// writeLocationNested applies a request's optional nested geo/links/dates to an
// existing location. It takes the Store to use rather than reading s.Store, so
// the callers can hand it a transaction-bound one and have the whole location
// commit or not at all.
func writeLocationNested(ctx context.Context, store db.Store, location db.Location, req locationRequest) error {
	if req.Geo != nil {
		if _, err := store.UpsertLocationGeo(ctx, db.UpsertLocationGeoParams{
			ID:         uuid.NewString(),
			LocationID: location.ID,
			Lat:        req.Geo.Lat,
			Lng:        req.Geo.Lng,
			Address:    req.Geo.Address,
			OSMType:    req.Geo.OSMType,
			OSMID:      req.Geo.OSMID,
		}); err != nil {
			return err
		}
	}

	if req.Links != nil {
		existing, err := store.ListLocationLinksByLocation(ctx, location.ID)
		if err != nil {
			return err
		}
		for _, l := range existing {
			if _, err := store.DeleteLocationLink(ctx, l.ID, location.ID); err != nil {
				return err
			}
		}
		for i, l := range *req.Links {
			if _, err := store.CreateLocationLink(ctx, db.CreateLocationLinkParams{
				ID:         uuid.NewString(),
				LocationID: location.ID,
				URL:        l.URL,
				Label:      l.Label,
				SortOrder:  i,
			}); err != nil {
				return err
			}
		}
	}

	if req.Dates != nil {
		if err := reconcileLocationDates(ctx, store, location, *req.Dates); err != nil {
			return err
		}
	}

	if req.Tags != nil {
		if err := writeLocationTags(ctx, store, location.ID, tags.Normalize(*req.Tags)); err != nil {
			return err
		}
	}

	return nil
}

func (s *Server) handleCreateLocation(w http.ResponseWriter, r *http.Request) {
	trip, _, ok := s.loadTrip(w, r, db.RoleEditor)
	if !ok {
		return
	}

	// A multipart body carries the cover photo and the files alongside the
	// location, so the whole location commits or does not -- see
	// locations_create.go. The JSON path below stays exactly as it was: it is
	// what the assistant and every other caller send, and readJSON's
	// unknown-field strictness is part of its contract.
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		s.createLocationMultipart(w, r, trip)
		return
	}

	var req locationRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Same transaction as the multipart path, with no image and no files.
	location, err := s.createLocationTx(r.Context(), trip, uuid.NewString(), req, nil, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create location")
		return
	}
	// The detail shape, not the bare location: a create can now carry nested
	// location/links/dates, and the client needs them (with their generated
	// IDs) back without a second GET.
	writeJSON(w, http.StatusCreated, s.buildLocationDetail(r, location))
}

func (s *Server) handleGetLocation(w http.ResponseWriter, r *http.Request) {
	location, _, ok := s.loadLocation(w, r, db.RoleViewer)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, s.buildLocationDetail(r, location))
}

func (s *Server) buildLocationDetail(r *http.Request, location db.Location) locationDetailResponse {
	detail := locationDetailResponse{locationResponse: s.locationToResponse(r.Context(), location), Links: []locationLinkResponse{}}

	if geo, err := s.Store.GetLocationGeoByLocationID(r.Context(), location.ID); err == nil {
		geoResp := newGeoResponse(geo)
		detail.Geo = &geoResp
	}

	if links, err := s.Store.ListLocationLinksByLocation(r.Context(), location.ID); err == nil {
		for _, l := range links {
			detail.Links = append(detail.Links, locationLinkResponse{ID: l.ID, URL: l.URL, Label: l.Label, SortOrder: l.SortOrder})
		}
	}

	// Tolerant in the same way, and for the same reason.
	if tags, err := s.Store.ListLocationTagsByLocation(r.Context(), location.ID); err == nil {
		detail.Tags = tags
	}

	// The days this location is on in the itinerary, collapsed into ranges.
	// Tolerant of a failure the way the two blocks above are: losing the dates
	// costs a card on the page, not the location.
	if rows, err := s.Store.ListItineraryDatesByLocation(r.Context(), location.ID); err == nil {
		dates := make([]string, len(rows))
		for i, row := range rows {
			dates[i] = row.Date
		}
		detail.Dates = collapseDateRanges(dates)
	}

	return detail
}

func (s *Server) handleUpdateLocation(w http.ResponseWriter, r *http.Request) {
	location, _, ok := s.loadLocation(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req locationRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	showOnMap := location.ShowOnMap
	if req.ShowOnMap != nil {
		showOnMap = *req.ShowOnMap
	}
	var updated db.Location
	err := s.Store.WithTx(r.Context(), func(store db.Store) error {
		saved, err := store.UpdateLocation(r.Context(), db.UpdateLocationParams{
			ID:        location.ID,
			TripID:    location.TripID,
			Category:  req.Category,
			Title:     req.Title,
			Notes:     req.Notes,
			ShowOnMap: showOnMap,
			UpdatedAt: time.Now().UTC(),
		})
		if err != nil {
			return err
		}
		if err := writeLocationNested(r.Context(), store, saved, req); err != nil {
			return err
		}
		updated = saved
		return nil
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update location")
		return
	}
	writeJSON(w, http.StatusOK, s.buildLocationDetail(r, updated))
}

func (s *Server) handleDeleteLocation(w http.ResponseWriter, r *http.Request) {
	location, _, ok := s.loadLocation(w, r, db.RoleEditor)
	if !ok {
		return
	}
	if _, err := s.Store.DeleteLocation(r.Context(), location.ID, location.TripID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete location")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type geoRequest struct {
	Lat     *float64 `json:"lat"`
	Lng     *float64 `json:"lng"`
	Address *string  `json:"address"`
	OSMType *string  `json:"osm_type"`
	OSMID   *string  `json:"osm_id"`
}

// osmElementTypes is what OpenStreetMap has, and there is no fourth.
var osmElementTypes = map[string]bool{"node": true, "way": true, "relation": true}

// validate checks the OpenStreetMap identity, which the client turns into
// https://www.openstreetmap.org/<type>/<id> and renders as an href.
//
// A security check in the same family as validateLinkURL, not a tidiness one.
// These two fields arrive from the client and are interpolated into a URL path,
// so an unchecked osm_type of "../../evil" or a javascript: payload would be
// rendered as a working link on a shared trip -- any editor could plant it and
// any member who clicked would run it with their own session. Constraining the
// values to what OSM actually has is a stronger defence than escaping, and it
// is available here in a way it is not for a free-text link.
//
// Both or neither. Half an identity cannot build a URL, and storing one half
// only invites a render site to interpolate an empty string into the path.
func (r geoRequest) validate() error {
	typeSet := r.OSMType != nil && strings.TrimSpace(*r.OSMType) != ""
	idSet := r.OSMID != nil && strings.TrimSpace(*r.OSMID) != ""
	if typeSet != idSet {
		return errors.New("osm_type and osm_id must be given together")
	}
	if !typeSet {
		return nil
	}
	if !osmElementTypes[*r.OSMType] {
		return errors.New("osm_type must be one of: node, way, relation")
	}
	if !isDigits(*r.OSMID) {
		return errors.New("osm_id must be a positive integer")
	}
	return nil
}

// isDigits reports whether s is a non-empty run of ASCII digits. An OSM element
// id is checked rather than parsed because it is stored and echoed as text and
// never used as a number -- and because a large way id parsed into a float
// would lose precision silently.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (s *Server) handlePutLocationGeo(w http.ResponseWriter, r *http.Request) {
	location, _, ok := s.loadLocation(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req geoRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// The same check the nested geo gets on location create/update: this
	// endpoint is a second door to the same columns.
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	geo, err := s.Store.UpsertLocationGeo(r.Context(), db.UpsertLocationGeoParams{
		ID:         uuid.NewString(),
		LocationID: location.ID,
		Lat:        req.Lat,
		Lng:        req.Lng,
		Address:    req.Address,
		OSMType:    req.OSMType,
		OSMID:      req.OSMID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save location")
		return
	}
	writeJSON(w, http.StatusOK, newGeoResponse(geo))
}

type locationLinkRequest struct {
	URL   string  `json:"url"`
	Label *string `json:"label"`
}

func (s *Server) handleCreateLocationLink(w http.ResponseWriter, r *http.Request) {
	location, _, ok := s.loadLocation(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req locationLinkRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	// The same check the nested-links path applies, for the same reason: this
	// endpoint writes to the same column and its rows reach the same href.
	if err := validateLinkURL(req.URL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	link, err := s.Store.CreateLocationLink(r.Context(), db.CreateLocationLinkParams{
		ID:         uuid.NewString(),
		LocationID: location.ID,
		URL:        req.URL,
		Label:      req.Label,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create link")
		return
	}
	writeJSON(w, http.StatusCreated, locationLinkResponse{ID: link.ID, URL: link.URL, Label: link.Label, SortOrder: link.SortOrder})
}

func (s *Server) handleDeleteLocationLink(w http.ResponseWriter, r *http.Request) {
	location, _, ok := s.loadLocation(w, r, db.RoleEditor)
	if !ok {
		return
	}
	linkID := chi.URLParam(r, "linkId")
	deleted, err := s.Store.DeleteLocationLink(r.Context(), linkID, location.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete link")
		return
	}
	if !deleted {
		writeError(w, http.StatusNotFound, "link not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSetLocationImage(w http.ResponseWriter, r *http.Request) {
	location, _, ok := s.loadLocation(w, r, db.RoleEditor)
	if !ok {
		return
	}

	var req setPreviewImageRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Same check as handleSetTripPreviewImage: the asset id arrives in the
	// body, so the route authorized the *location*, not the asset. A nil id
	// clears the image and names nothing to check.
	if req.MediaAssetID != nil {
		asset, err := s.Store.GetMediaAssetByID(r.Context(), *req.MediaAssetID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				writeError(w, http.StatusBadRequest, "media asset not found")
			} else {
				writeError(w, http.StatusInternalServerError, "could not load media asset")
			}
			return
		}
		if !s.requireSameTrip(w, asset.TripID, location.TripID, "media asset belongs to another trip") {
			return
		}
	}

	updated, err := s.Store.SetLocationImage(r.Context(), location.ID, location.TripID, req.MediaAssetID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not set image")
		return
	}
	writeJSON(w, http.StatusOK, s.locationToResponse(r.Context(), updated))
}
