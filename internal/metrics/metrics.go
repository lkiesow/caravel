// Package metrics is the instance's Prometheus instrumentation, served by
// GET /metrics in the OpenMetrics text format.
//
// Everything lives on a registry of its own rather than the client library's
// global default. A disabled instance then builds nothing and registers
// nothing, and a test can build as many Metrics as it likes without the
// "duplicate metrics collector registration" panic the global one would give
// the second.
//
// A nil *Metrics is valid and means "off": Middleware passes requests straight
// through. That is what keeps the HTTP layer free of "if metrics enabled"
// branches -- it holds a possibly-nil pointer and calls it unconditionally.
package metrics

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"caravel/internal/buildinfo"
	"caravel/internal/db"
	"caravel/internal/storagefs"
)

// Source is what metrics reads from the store on each scrape. The two methods
// rather than db.Store, so a test can hand it a small fake.
type Source interface {
	CountActiveSessions(ctx context.Context, now time.Time) (int64, error)
	InstanceCounts(ctx context.Context) (db.InstanceCounts, error)
}

// queryTimeout bounds each query a scrape triggers. A scrape that waits on a
// locked SQLite database should report those gauges as missing, not hold the
// scraper until its own timeout fires and lose every other metric too.
const queryTimeout = 2 * time.Second

// staticRoute labels requests that matched no route and fell through to the
// static file server. Their raw paths are unbounded -- every versioned asset
// URL carries a hash -- so they share a single label value.
const staticRoute = "static"

type Metrics struct {
	registry *prometheus.Registry

	httpRequests *prometheus.CounterVec
	httpDuration *prometheus.HistogramVec
	blobWrites   *prometheus.CounterVec
	blobBytes    prometheus.Histogram

	handler http.Handler
}

// New builds the instrumentation and registers it, including the collectors
// that read conn's pool statistics and query source on every scrape.
func New(conn *sql.DB, source Source) *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		httpRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "caravel_http_requests_total",
			Help: "HTTP requests handled, by method, route pattern and status code.",
		}, []string{"method", "route", "code"}),
		httpDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "caravel_http_request_duration_seconds",
			Help:    "Time to handle an HTTP request, by method and route pattern.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
		blobWrites: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "caravel_blob_writes_total",
			Help: "Files written to blob storage (uploads and the images derived from them), by result.",
		}, []string{"result"}),
		blobBytes: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "caravel_blob_write_bytes",
			Help: "Size of each file successfully written to blob storage.",
			// 1 KiB to 64 MiB in powers of four, which brackets everything
			// from a resized thumbnail to the 50 MB upload cap.
			Buckets: prometheus.ExponentialBuckets(1<<10, 4, 9),
		}),
	}

	buildInfo := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "caravel_build_info",
		Help: "Always 1; the version label names the running build.",
	}, []string{"version"})
	buildInfo.WithLabelValues(buildinfo.Version).Set(1)

	activeSessions := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "caravel_sessions_active",
		Help: "Sessions that have not yet expired.",
	}, func() float64 {
		ctx, cancel := context.WithTimeout(context.Background(), queryTimeout)
		defer cancel()
		n, err := source.CountActiveSessions(ctx, time.Now().UTC())
		if err != nil {
			// NaN rather than zero: zero is a real answer ("nobody is logged
			// in") and would be believed.
			slog.Debug("metrics: count active sessions", "err", err)
			return math.NaN()
		}
		return float64(n)
	})

	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewDBStatsCollector(conn, "caravel"),
		buildInfo,
		activeSessions,
		dataCollector{source: source},
		m.httpRequests,
		m.httpDuration,
		m.blobWrites,
		m.blobBytes,
	)
	// Compression is left to the router's own middleware.Compress, which
	// already wraps every response: with both on, a scraper asking for gzip
	// would get it twice.
	m.handler = promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{
		EnableOpenMetrics:  true,
		DisableCompression: true,
	})
	return m
}

// Handler serves the registry in the Prometheus text format, or in
// OpenMetrics to a scraper that asks for it.
func (m *Metrics) Handler() http.Handler { return m.handler }

// Middleware counts and times every request.
//
// The route label is chi's matched pattern ("/api/trips/{tripId}"), read
// after the handler has run because that is when chi has finished filling it
// in. The raw path would put every trip ID into a label value and grow the
// series without bound.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	if m == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		route := staticRoute
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			if p := rctx.RoutePattern(); p != "" {
				route = p
			}
		}
		code := ww.Status()
		if code == 0 {
			// Nothing was written at all, which net/http answers as 200.
			code = http.StatusOK
		}
		m.httpRequests.WithLabelValues(r.Method, route, strconv.Itoa(code)).Inc()
		m.httpDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}

// InstrumentBlob wraps a blob store so that every Put is counted and sized.
// Every upload path ends in a Put, so this is the one place that sees them
// all without each handler having to remember to report.
func (m *Metrics) InstrumentBlob(b storagefs.Blob) storagefs.Blob {
	if m == nil {
		return b
	}
	return &instrumentedBlob{Blob: b, m: m}
}

type instrumentedBlob struct {
	storagefs.Blob
	m *Metrics
}

func (b *instrumentedBlob) Put(ctx context.Context, key string, r io.Reader) (int64, error) {
	size, err := b.Blob.Put(ctx, key, r)
	if err != nil {
		b.m.blobWrites.WithLabelValues("error").Inc()
		return size, err
	}
	b.m.blobWrites.WithLabelValues("ok").Inc()
	b.m.blobBytes.Observe(float64(size))
	return size, nil
}
