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

	"caravel/internal/db"
	"caravel/internal/dbtest"
	"caravel/internal/storagefs"
)

// fakeSource answers whatever its funcs say; a nil func answers zero.
type fakeSource struct {
	sessions func() (int64, error)
	counts   func() (db.InstanceCounts, error)
}

func (f fakeSource) CountActiveSessions(context.Context, time.Time) (int64, error) {
	if f.sessions == nil {
		return 0, nil
	}
	return f.sessions()
}

func (f fakeSource) InstanceCounts(context.Context) (db.InstanceCounts, error) {
	if f.counts == nil {
		return db.InstanceCounts{}, nil
	}
	return f.counts()
}

func newMetrics(t *testing.T, source fakeSource) *Metrics {
	t.Helper()
	_, conn := dbtest.Open(t)
	return New(conn, source)
}

func scrapeBody(t *testing.T, m *Metrics) string {
	t.Helper()
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("scrape status = %d, want 200", rec.Code)
	}
	return rec.Body.String()
}

func TestMiddlewareLabelsByRoutePattern(t *testing.T) {
	m := newMetrics(t, fakeSource{})

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
	m := newMetrics(t, fakeSource{})

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
	m := newMetrics(t, fakeSource{sessions: func() (int64, error) { return count, err }})

	scrape := func() string { return scrapeBody(t, m) }

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

func TestDataGauges(t *testing.T) {
	counts := db.InstanceCounts{
		Users: 2, Trips: 5, Files: 3, FileBytes: 123456, Expenses: 7,
		LocationsByCategory: map[string]int64{"stay": 4, "site": 9},
	}
	var err error
	m := newMetrics(t, fakeSource{counts: func() (db.InstanceCounts, error) { return counts, err }})

	body := scrapeBody(t, m)
	for _, want := range []string{
		"caravel_users 2",
		"caravel_trips 5",
		`caravel_locations{category="site"} 9`,
		`caravel_locations{category="stay"} 4`,
		"caravel_files 3",
		"caravel_files_size_bytes 123456",
		"caravel_expenses 7",
	} {
		if !strings.Contains(body, want+"\n") {
			t.Errorf("scrape lacks %q", want)
		}
	}

	// A failed read drops the data gauges but keeps the scrape: the request
	// and runtime metrics matter most when the database is the problem.
	err = errors.New("database is locked")
	body = scrapeBody(t, m)
	if strings.Contains(body, "caravel_trips ") {
		t.Error("data gauges reported despite the failed read")
	}
	if !strings.Contains(body, "go_goroutines") {
		t.Error("a failed data read took the rest of the scrape with it")
	}
}
