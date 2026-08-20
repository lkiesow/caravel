package httpapi

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// The trip-level Files list must show files attached to a location as well as
// files on the trip itself, each labelled with the location it belongs to.
//
// It used to filter `item_id IS NULL`, so a booking uploaded to a hotel was
// invisible on the trip's Files tab even though it is a file on that trip -
// reachable only by knowing which location to open.
func TestListTripFilesIncludesLocationFiles(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")
	itemID := ts.createItem(cookie, tripID, "Foss Hotel Reykjavik")

	if w := ts.upload("/api/trips/"+tripID+"/files", cookie, "trip-notes.txt", "text/plain", []byte("trip level")); w.Code != http.StatusCreated {
		t.Fatalf("upload trip file: %d %s", w.Code, w.Body.String())
	}
	if w := ts.upload("/api/items/"+itemID+"/files", cookie, "hotel-booking.txt", "text/plain", []byte("item level")); w.Code != http.StatusCreated {
		t.Fatalf("upload item file: %d %s", w.Code, w.Body.String())
	}

	w := ts.do(http.MethodGet, "/api/trips/"+tripID+"/files", cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list trip files: %d %s", w.Code, w.Body.String())
	}
	files := decode[[]fileResponse](t, w)
	if len(files) != 2 {
		t.Fatalf("got %d files, want 2 (trip-level + location-attached): %s", len(files), w.Body.String())
	}

	byName := map[string]fileResponse{}
	for _, d := range files {
		byName[d.Filename] = d
	}

	tripDoc, ok := byName["trip-notes.txt"]
	if !ok {
		t.Fatal("the trip-level file is missing from the list")
	}
	if tripDoc.ItemID != nil {
		t.Errorf("trip-level file has item_id %q, want null", *tripDoc.ItemID)
	}
	// The LEFT JOIN's whole point: a trip-level row has no item to join to and
	// must still come back. An INNER JOIN would have dropped this one.
	if tripDoc.ItemTitle != nil {
		t.Errorf("trip-level file has item_title %q, want null", *tripDoc.ItemTitle)
	}

	itemDoc, ok := byName["hotel-booking.txt"]
	if !ok {
		t.Fatal("the location-attached file is missing from the list — this is the bug this test exists for")
	}
	if itemDoc.ItemID == nil || *itemDoc.ItemID != itemID {
		t.Errorf("location-attached file has item_id %v, want %q", itemDoc.ItemID, itemID)
	}
	if itemDoc.ItemTitle == nil || *itemDoc.ItemTitle != "Foss Hotel Reykjavik" {
		t.Errorf("location-attached file has item_title %v, want %q", itemDoc.ItemTitle, "Foss Hotel Reykjavik")
	}

	// The location's own list is unchanged: its file, and not the trip's.
	w = ts.do(http.MethodGet, "/api/items/"+itemID+"/files", cookie, "")
	itemDocs := decode[[]fileResponse](t, w)
	if len(itemDocs) != 1 || itemDocs[0].Filename != "hotel-booking.txt" {
		t.Fatalf("location's own list should hold exactly its own file, got %s", w.Body.String())
	}
	// item_title is left null off the trip listing: on a location's own page
	// every row belongs to that location, so labelling each one is noise.
	if itemDocs[0].ItemTitle != nil {
		t.Errorf("item-level list set item_title %q, want null", *itemDocs[0].ItemTitle)
	}

	// Deleting a location-attached file from the trip list works: DeleteFile
	// scopes by (id, trip_id), which holds for both kinds of row - worth
	// asserting rather than assuming, since the trip list can now offer a delete
	// for a file it doesn't directly own.
	if w := ts.do(http.MethodDelete, "/api/files/"+itemDoc.ID, cookie, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete location-attached file: %d %s", w.Code, w.Body.String())
	}
	w = ts.do(http.MethodGet, "/api/trips/"+tripID+"/files", cookie, "")
	if remaining := decode[[]fileResponse](t, w); len(remaining) != 1 || remaining[0].Filename != "trip-notes.txt" {
		t.Fatalf("after deleting the location file, got %s", w.Body.String())
	}
}

func TestIsInlineSafeContentType(t *testing.T) {
	cases := []struct {
		contentType string
		want        bool
	}{
		{"application/pdf", true},
		{"image/png", true},
		{"image/jpeg", true},
		{"text/plain; charset=utf-8", true},
		{"image/svg+xml", false},
		{"application/zip", false},
		{"text/html", false},
	}
	for _, c := range cases {
		if got := isInlineSafeContentType(c.contentType); got != c.want {
			t.Errorf("isInlineSafeContentType(%q) = %v, want %v", c.contentType, got, c.want)
		}
	}
}

func TestSniffContentType(t *testing.T) {
	pdfMagic := "%PDF-1.4\n" + strings.Repeat("x", 100)
	r := strings.NewReader(pdfMagic)
	got, err := sniffContentType(r)
	if err != nil {
		t.Fatalf("sniffContentType: %v", err)
	}
	if got != "application/pdf" {
		t.Errorf("got %q, want application/pdf", got)
	}

	// Confirm the reader was rewound so the caller can still read the full content.
	rest, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read after sniff: %v", err)
	}
	if string(rest) != pdfMagic {
		t.Errorf("reader was not rewound to the start after sniffing")
	}
}
