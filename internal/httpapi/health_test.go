package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"caravel/internal/buildinfo"
)

// The health endpoint is what a deploy check, a test or a person asks "is this
// up, and which build is it?" - so both halves of the answer are asserted, and
// the version is compared against buildinfo rather than a literal: the whole
// point is that it reports whatever the linker stamped in, not a constant this
// test could keep passing against after the stamping broke.
//
// Unauthenticated on purpose: no cookie is passed here, and a 200 is expected.
func TestHealthReportsStatusAndVersion(t *testing.T) {
	ts := newTestServer(t)

	w := ts.do(http.MethodGet, "/api/health", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/health: got %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type: got %q, want application/json", ct)
	}

	var got healthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body %q: %v", w.Body.String(), err)
	}
	if got.Status != "ok" {
		t.Errorf("status: got %q, want %q", got.Status, "ok")
	}
	if got.Version != buildinfo.Version {
		t.Errorf("version: got %q, want %q", got.Version, buildinfo.Version)
	}
	if got.Version == "" {
		t.Error("version is empty — a build with no -ldflags should still report the \"dev\" default")
	}
}
