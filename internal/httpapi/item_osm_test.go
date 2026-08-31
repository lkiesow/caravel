package httpapi

import (
	"net/http"
	"testing"
)

// Stage 29 Milestone 3. The OpenStreetMap identity becomes an href on the
// client, so it is validated on the way in rather than escaped on the way out:
// constraining the value to the three element types OSM actually has is a
// stronger defence than escaping, and unlike a free-text link it is available.
func TestItemLocationRequestValidate(t *testing.T) {
	s := func(v string) *string { return &v }

	cases := []struct {
		name    string
		req     itemLocationRequest
		wantErr string
	}{
		{
			name: "neither field is the common case and is fine",
			req:  itemLocationRequest{},
		},
		{
			name: "a node",
			req:  itemLocationRequest{OSMType: s("node"), OSMID: s("240109189")},
		},
		{
			name: "a way",
			req:  itemLocationRequest{OSMType: s("way"), OSMID: s("1234567890123")},
		},
		{
			name: "a relation",
			req:  itemLocationRequest{OSMType: s("relation"), OSMID: s("7")},
		},
		{
			name:    "a type without an id",
			req:     itemLocationRequest{OSMType: s("node")},
			wantErr: "osm_type and osm_id must be given together",
		},
		{
			name:    "an id without a type",
			req:     itemLocationRequest{OSMID: s("1")},
			wantErr: "osm_type and osm_id must be given together",
		},
		{
			// Whitespace is not a value: an empty string in a URL path would
			// produce openstreetmap.org/node/ and read as a broken link.
			name:    "a blank type counts as absent, so the id is now half an identity",
			req:     itemLocationRequest{OSMType: s("   "), OSMID: s("1")},
			wantErr: "osm_type and osm_id must be given together",
		},
		{
			name:    "an element type OSM does not have",
			req:     itemLocationRequest{OSMType: s("nonsense"), OSMID: s("1")},
			wantErr: "osm_type must be one of: node, way, relation",
		},
		{
			// The reason this is a security check and not a tidiness one.
			name:    "path traversal in the type",
			req:     itemLocationRequest{OSMType: s("../../evil"), OSMID: s("1")},
			wantErr: "osm_type must be one of: node, way, relation",
		},
		{
			name:    "a scheme smuggled through the type",
			req:     itemLocationRequest{OSMType: s("javascript:alert(1)//"), OSMID: s("1")},
			wantErr: "osm_type must be one of: node, way, relation",
		},
		{
			name:    "case matters, because OSM element types are lowercase",
			req:     itemLocationRequest{OSMType: s("Node"), OSMID: s("1")},
			wantErr: "osm_type must be one of: node, way, relation",
		},
		{
			name:    "an id that is not digits",
			req:     itemLocationRequest{OSMType: s("node"), OSMID: s("1; DROP TABLE items")},
			wantErr: "osm_id must be a positive integer",
		},
		{
			name:    "a negative id",
			req:     itemLocationRequest{OSMType: s("node"), OSMID: s("-1")},
			wantErr: "osm_id must be a positive integer",
		},
		{
			name:    "a query string appended to the id",
			req:     itemLocationRequest{OSMType: s("node"), OSMID: s("1?x=y")},
			wantErr: "osm_id must be a positive integer",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.validate()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Fatalf("expected error %q, got nil", tc.wantErr)
			case tc.wantErr != "" && err.Error() != tc.wantErr:
				t.Fatalf("got error %q, want %q", err, tc.wantErr)
			}
		})
	}
}

// The identity has to survive the round trip through both write doors -- the
// nested location on item create/update, and the standalone PUT
// /items/{id}/location -- because the client reads it back to decide whether
// to render the OpenStreetMap link at all.
func TestItemLocationOSMIdentityRoundTrips(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("alice")
	tripID := ts.createTrip(cookie, "Iceland")

	type locationBody struct {
		OSMType *string `json:"osm_type"`
		OSMID   *string `json:"osm_id"`
	}

	body := `{"title":"Hallgrimskirkja","category":"site","location":{"lat":64.1417951,"lng":-21.9267103,` +
		`"address":"Hallgrimstorg 1","osm_type":"way","osm_id":"1234567890123"}}`
	itemID := ts.mustCreate(http.MethodPost, "/api/trips/"+tripID+"/items", cookie, body, http.StatusCreated)

	detail := decode[struct {
		Location locationBody `json:"location"`
	}](t, ts.do(http.MethodGet, "/api/items/"+itemID, cookie, ""))
	if detail.Location.OSMType == nil || *detail.Location.OSMType != "way" {
		t.Errorf("osm_type after create = %v, want way", detail.Location.OSMType)
	}
	if detail.Location.OSMID == nil || *detail.Location.OSMID != "1234567890123" {
		t.Errorf("osm_id after create = %v, want 1234567890123", detail.Location.OSMID)
	}

	// The standalone door, which is a second way into the same columns and had
	// to grow the same validation.
	w := ts.do(http.MethodPut, "/api/items/"+itemID+"/location", cookie,
		`{"lat":1,"lng":2,"osm_type":"node","osm_id":"240109189"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("put location: status = %d, body %s", w.Code, w.Body.String())
	}
	if put := decode[locationBody](t, w); put.OSMType == nil || *put.OSMType != "node" {
		t.Errorf("osm_type after put = %v, want node", put.OSMType)
	}

	// A bad value is a 400 on that door too, not a stored href.
	if w := ts.do(http.MethodPut, "/api/items/"+itemID+"/location", cookie,
		`{"lat":1,"lng":2,"osm_type":"../../evil","osm_id":"1"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("put location with a bad type: status = %d, want 400, body %s", w.Code, w.Body.String())
	}

	// Moving the point without an identity clears it, which is what the editor
	// does when the pin is dragged: a stale identity is a link to the wrong
	// place, which is worse than no link.
	w = ts.do(http.MethodPut, "/api/items/"+itemID+"/location", cookie, `{"lat":9,"lng":9}`)
	if w.Code != http.StatusOK {
		t.Fatalf("put location without an identity: status = %d, body %s", w.Code, w.Body.String())
	}
	if cleared := decode[locationBody](t, w); cleared.OSMType != nil || cleared.OSMID != nil {
		t.Errorf("identity should be cleared, got %v/%v", cleared.OSMType, cleared.OSMID)
	}
}
