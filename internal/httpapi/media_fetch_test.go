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

// paddedPNG is a valid little PNG followed by n bytes of nothing. Decoders
// ignore whatever trails IEND, so this is an image of an arbitrary size
// without the cost of actually encoding one -- which is what lets the two
// tests below sit either side of the limit cheaply.
func paddedPNG(t *testing.T, n int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{R: 20, G: 120, B: 200, A: 255})
	var body bytes.Buffer
	if err := png.Encode(&body, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	body.Write(make([]byte, n))
	return body.Bytes()
}

func serveBytes(t *testing.T, body []byte) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		// The write is expected to fail once the fetcher stops reading at the
		// limit, so the error is deliberately not reported.
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/cover.png"
}

// A 20MB image used to be refused outright: the cap was 15MB, even though the
// image is downscaled to imaging.MaxDimension and re-encoded before storage,
// so the size of what arrives says nothing about the size of what is kept.
// Reported from the location editor, where picking a photo off the web failed
// with "image exceeds maximum size of 15728640 bytes".
func TestFetchImageAcceptsAnImageOverTheOldLimit(t *testing.T) {
	res, err := fetchImage(context.Background(), serveBytes(t, paddedPNG(t, 20<<20)))
	if err != nil {
		t.Fatalf("fetchImage: %v", err)
	}
	if len(res.Data) > 1<<20 {
		t.Errorf("stored %d bytes; the downscale and re-encode should have made this tiny", len(res.Data))
	}
}

func TestFetchImageStillRefusesAnImageOverTheLimit(t *testing.T) {
	_, err := fetchImage(context.Background(), serveBytes(t, paddedPNG(t, maxImageUploadBytes+1)))
	if err == nil {
		t.Fatal("fetchImage accepted a body over maxImageUploadBytes")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Errorf("error = %q, want the size limit rather than something incidental", err)
	}
}
