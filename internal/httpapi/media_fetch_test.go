package httpapi

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Fetching an image from a URL, and the header that decides whether it works.
//
// upload.wikimedia.org answers the default Go User-Agent with 403 -- their
// published policy, not a rate limit -- so the first cover the assistant
// found on Wikipedia could not be downloaded even though the same URL opened
// fine in a browser. The server below behaves the way theirs does.
func TestFetchImageIdentifiesItselfToHostsThatInsist(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{R: 20, G: 120, B: 200, A: 255})
	var body bytes.Buffer
	if err := png.Encode(&body, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}

	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("User-Agent")
		if seen == "" || strings.HasPrefix(seen, "Go-http-client/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(body.Bytes())
	}))
	defer srv.Close()

	if _, err := fetchImage(context.Background(), srv.URL+"/cover.png"); err != nil {
		t.Fatalf("fetchImage: %v (User-Agent was %q)", err, seen)
	}
	if !strings.Contains(seen, "Caravel") {
		t.Errorf("User-Agent = %q, want one that names this application", seen)
	}
}
