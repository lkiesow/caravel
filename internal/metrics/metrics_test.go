package metrics

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"caravel/internal/dbtest"
	"caravel/internal/storagefs"
)

type sessionCount func() (int64, error)

func (f sessionCount) CountActiveSessions(context.Context, time.Time) (int64, error) { return f() }

func newMetrics(t *testing.T, sessions sessionCount) *Metrics {
	t.Helper()
	_, conn := dbtest.Open(t)
	return New(conn, sessions)
}

func TestMiddlewareLabelsByRoutePattern(t *testing.T) {
	m := newMetrics(t, func() (int64, error) { return 0, nil })

	r := chi.NewRouter()
	r.Use(m.Middleware)
	r.Get("/api/trips/{tripId}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		// The static fallback: writes a body and never sets a status.
		_, _ = io.WriteString(w, "<!doctype html>")
	})

	for _, path := range []string{"/api/trips/a", "/api/trips/b", "/v/0123abcd/js/app.js", "/trips/xyz"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	// Two trips, one series: the label is the pattern, not the path.
	if got := testutil.ToFloat64(m.httpRequests.WithLabelValues("GET", "/api/trips/{tripId}", "418")); got != 2 {
		t.Errorf("trip route count = %v, want 2", got)
	}
	// Everything unmatched collapses into one label, and a handler that only
	// wrote a body is counted as the 200 net/http sent.
	if got := testutil.ToFloat64(m.httpRequests.WithLabelValues("GET", staticRoute, "200")); got != 2 {
		t.Errorf("static count = %v, want 2", got)
	}
	if n := testutil.CollectAndCount(m.httpRequests); n != 2 {
		t.Errorf("request series = %d, want 2 (no raw paths as labels)", n)
	}
	if n := testutil.CollectAndCount(m.httpDuration); n != 2 {
		t.Errorf("duration series = %d, want 2", n)
	}
}

func TestNilMetricsIsOff(t *testing.T) {
	var m *Metrics
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if got := m.Middleware(h); got == nil {
		t.Fatal("nil Metrics returned a nil handler")
	}
	b := storagefs.NewLocalFS(t.TempDir())
	if got := m.InstrumentBlob(b); got != b {
		t.Error("nil Metrics wrapped the blob store; want it returned unchanged")
	}
}

type failingBlob struct{ storagefs.Blob }

func (failingBlob) Put(context.Context, string, io.Reader) (int64, error) {
	return 0, errors.New("disk full")
}

func TestInstrumentBlob(t *testing.T) {
	m := newMetrics(t, func() (int64, error) { return 0, nil })

	ok := m.InstrumentBlob(storagefs.NewLocalFS(t.TempDir()))
	for _, size := range []int{3000, 70000} {
		if _, err := ok.Put(context.Background(), "k", bytes.NewReader(make([]byte, size))); err != nil {
			t.Fatalf("put: %v", err)
		}
	}
	bad := m.InstrumentBlob(failingBlob{})
	if _, err := bad.Put(context.Background(), "k", strings.NewReader("x")); err == nil {
		t.Fatal("failing put reported success")
	}

	if got := testutil.ToFloat64(m.blobWrites.WithLabelValues("ok")); got != 2 {
		t.Errorf("ok writes = %v, want 2", got)
	}
	if got := testutil.ToFloat64(m.blobWrites.WithLabelValues("error")); got != 1 {
		t.Errorf("error writes = %v, want 1", got)
	}
	// Only successful writes are sized.
	want := `
# HELP caravel_blob_write_bytes Size of each file successfully written to blob storage.
# TYPE caravel_blob_write_bytes histogram
caravel_blob_write_bytes_bucket{le="1024"} 0
caravel_blob_write_bytes_bucket{le="4096"} 1
caravel_blob_write_bytes_bucket{le="16384"} 1
caravel_blob_write_bytes_bucket{le="65536"} 1
caravel_blob_write_bytes_bucket{le="262144"} 2
caravel_blob_write_bytes_bucket{le="1.048576e+06"} 2
caravel_blob_write_bytes_bucket{le="4.194304e+06"} 2
caravel_blob_write_bytes_bucket{le="1.6777216e+07"} 2
caravel_blob_write_bytes_bucket{le="6.7108864e+07"} 2
caravel_blob_write_bytes_bucket{le="+Inf"} 2
caravel_blob_write_bytes_sum 73000
caravel_blob_write_bytes_count 2
`
	if err := testutil.CollectAndCompare(m.blobBytes, strings.NewReader(want)); err != nil {
		t.Error(err)
	}
}

func TestSessionsGauge(t *testing.T) {
	count, err := int64(3), error(nil)
	m := newMetrics(t, func() (int64, error) { return count, err })

	scrape := func() string {
		rec := httptest.NewRecorder()
		m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		return rec.Body.String()
	}

	body := scrape()
	for _, want := range []string{"caravel_sessions_active 3", "caravel_build_info{version=", `go_sql_open_connections{db_name="caravel"}`, "go_goroutines"} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape lacks %q", want)
		}
	}

	// A failed count is reported as unknown, never as a believable zero.
	err = errors.New("database is locked")
	if body := scrape(); !strings.Contains(body, "caravel_sessions_active NaN") {
		t.Errorf("failed count not reported as NaN:\n%s", grepLine(body, "caravel_sessions_active "))
	}
}

func grepLine(s, prefix string) string {
	for line := range strings.SplitSeq(s, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return "(absent)"
}
