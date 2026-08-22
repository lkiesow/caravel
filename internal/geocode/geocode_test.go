package geocode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The behaviour that arrived with the lift: nil is a valid Client meaning
// "disabled", and calling it fails legibly rather than panicking. The mapping
// itself is covered end to end through the handler in
// internal/httpapi/geocode_test.go, which is where it was tested before the
// move and stayed green through it.

func TestNewReturnsNilForAnEmptyEndpoint(t *testing.T) {
	if c := New(""); c != nil {
		t.Errorf("New(\"\") = %v, want nil — nil is the off switch", c)
	}
	if c := New("http://example.invalid/search"); c == nil {
		t.Error("New(url) = nil, want a client")
	}
}

func TestSearchOnNilClientIsAnErrorNotAPanic(t *testing.T) {
	var c *Client
	// A missed nil check should fail where it happens, not three frames later
	// in a nil dereference.
	_, err := c.Search(context.Background(), "Reykjavik")
	if !errors.Is(err, ErrNotConfigured) {
		t.Errorf("err = %v, want ErrNotConfigured", err)
	}
}

func TestSearchSkipsUnparseableRowsRatherThanFailing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[
		  {"display_name":"Good","lat":"64.1","lon":"-21.9"},
		  {"display_name":"Bad coords","lat":"north","lon":"-21.9"},
		  {"display_name":"","lat":"64.2","lon":"-21.8"},
		  {"display_name":"Also good","lat":"64.3","lon":"-21.7"}
		]`)
	}))
	defer srv.Close()

	got, err := New(srv.URL).Search(context.Background(), "Reykjavik")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want the 2 usable ones: %+v", len(got), got)
	}
	if got[0].DisplayName != "Good" || got[1].DisplayName != "Also good" {
		t.Errorf("results = %+v", got)
	}
}

func TestSearchReturnsEmptyNotNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	// "searched, found nothing" must be distinguishable from "did not search",
	// and the handler serialises this straight to JSON where nil becomes null.
	got, err := New(srv.URL).Search(context.Background(), "zzzz")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got == nil {
		t.Error("Search returned nil, want an empty slice")
	}
}

func TestSearchSendsAnIdentifyingUserAgent(t *testing.T) {
	var ua string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua = r.Header.Get("User-Agent")
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()

	// A condition of using OSM's public instance, not politeness: anonymous
	// bulk traffic is what gets blocked.
	if _, err := New(srv.URL).Search(context.Background(), "Reykjavik"); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if ua == "" || ua == "Go-http-client/1.1" {
		t.Errorf("User-Agent = %q, want an identifying one", ua)
	}
}
