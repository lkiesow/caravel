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

// A note is the one thing about a file that can change after upload: it is the
// readable name a file gets when its own filename is a storage blob
// ("5d2ffd5f-...-173d5a72f860.png"), and until Stage 11 it could only be set in
// the upload form - so a file uploaded without one kept none forever.
//
// The clearing half is what this test is really for. "Cleared" has to mean SQL
// NULL, not the empty string: the Files list renders the filename as the card
// title when note is null, and an empty-string note would leave the row
// claiming to have a note whose text is "" - a state no UI can show and no
// user can get out of.
func TestUpdateFileNote(t *testing.T) {
	ts := newTestServer(t)
	cookie := ts.login("demo")
	tripID := ts.createTrip(cookie, "Iceland")

	w := ts.upload("/api/trips/"+tripID+"/files", cookie, "passport.png", "image/png", []byte("not really a png"))
	if w.Code != http.StatusCreated {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	fileID := decode[fileResponse](t, w).ID

	// Uploaded with no note at all, which is the state the endpoint exists to
	// get a file out of.
	if got := ts.getFile(t, cookie, tripID, fileID).Note; got != nil {
		t.Fatalf("freshly uploaded file has note %q, want null", *got)
	}

	for _, tc := range []struct {
		name string
		body string
		want *string // nil = the column must be NULL
	}{
		{"set", `{"note":"Copy of my identity card"}`, ptr("Copy of my identity card")},
		{"change", `{"note":"Passport scan"}`, ptr("Passport scan")},
		{"trimmed", `{"note":"  Passport scan  "}`, ptr("Passport scan")},
		{"cleared by empty string", `{"note":""}`, nil},
		{"cleared by whitespace", `{"note":"   "}`, nil},
		{"cleared by null", `{"note":null}`, nil},
		// One field, so an absent note can only mean the same as a null one.
		{"cleared by omission", `{}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Start from a set note every time, so the clearing cases are
			// actually clearing something.
			if w := ts.do(http.MethodPatch, "/api/files/"+fileID, cookie, `{"note":"before"}`); w.Code != http.StatusOK {
				t.Fatalf("seed note: %d %s", w.Code, w.Body.String())
			}

			w := ts.do(http.MethodPatch, "/api/files/"+fileID, cookie, tc.body)
			if w.Code != http.StatusOK {
				t.Fatalf("patch %s: %d %s", tc.body, w.Code, w.Body.String())
			}
			assertNote(t, "response", decode[fileResponse](t, w).Note, tc.want)
			// The response is the updated row, but a handler that echoed its
			// own input would pass that on its own - so re-read it.
			assertNote(t, "re-read", ts.getFile(t, cookie, tripID, fileID).Note, tc.want)
		})
	}

	// Everything else about the file is untouched, and item_title stays null on
	// this endpoint the way it does on every non-list one.
	after := ts.getFile(t, cookie, tripID, fileID)
	if after.Filename != "passport.png" || after.SizeBytes != int64(len("not really a png")) || after.ItemTitle != nil {
		t.Errorf("patching the note changed something else: %+v", after)
	}

	if w := ts.do(http.MethodPatch, "/api/files/00000000-0000-0000-0000-000000000000", cookie, `{"note":"x"}`); w.Code != http.StatusNotFound {
		t.Errorf("patch a file that doesn't exist: got %d, want 404", w.Code)
	}
	if w := ts.do(http.MethodPatch, "/api/files/"+fileID, cookie, `not json`); w.Code != http.StatusBadRequest {
		t.Errorf("patch with a broken body: got %d, want 400", w.Code)
	}
}

// getFile re-reads one file from the trip listing, so assertions run against
// what a client would actually see rather than against the PATCH response.
func (ts *testServer) getFile(t *testing.T, cookie *http.Cookie, tripID, fileID string) fileResponse {
	t.Helper()
	w := ts.do(http.MethodGet, "/api/trips/"+tripID+"/files", cookie, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list files: %d %s", w.Code, w.Body.String())
	}
	for _, f := range decode[[]fileResponse](t, w) {
		if f.ID == fileID {
			return f
		}
	}
	t.Fatalf("file %s is missing from the trip listing", fileID)
	return fileResponse{}
}

func assertNote(t *testing.T, where string, got, want *string) {
	t.Helper()
	switch {
	case want == nil && got != nil:
		t.Errorf("%s: note is %q, want null", where, *got)
	case want != nil && got == nil:
		t.Errorf("%s: note is null, want %q", where, *want)
	case want != nil && *got != *want:
		t.Errorf("%s: note is %q, want %q", where, *got, *want)
	}
}

func ptr(s string) *string { return &s }
