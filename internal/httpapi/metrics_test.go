package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"caravel/internal/metrics"
)

const testMetricsToken = "0123456789abcdef0123456789abcdef"

func newMetricsTestServer(t *testing.T, token string) *testServer {
	t.Helper()
	return newTestServerWithOptions(t, func(o *Options) {
		o.Metrics = metrics.New(o.DB, o.Store)
		o.MetricsToken = token
	})
}

func scrape(ts *testServer, authorization string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	ts.ServeHTTP(rec, req)
	return rec
}

// Off must mean a 404 and nothing else -- in particular not the page shell
// with a 200, which is what an unregistered path falls through to and which a
// scraper would read as an empty, healthy target.
func TestMetricsOffAnswers404(t *testing.T) {
	for name, ts := range map[string]*testServer{
		"no metrics":        newTestServer(t),
		"metrics, no token": newMetricsTestServer(t, ""),
	} {
		t.Run(name, func(t *testing.T) {
			rec := scrape(ts, "Bearer "+testMetricsToken)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "<") {
				t.Errorf("404 body looks like HTML: %q", rec.Body.String())
			}
		})
	}
}

func TestMetricsRequiresToken(t *testing.T) {
	ts := newMetricsTestServer(t, testMetricsToken)

	for name, header := range map[string]string{
		"missing":          "",
		"wrong":            "Bearer " + strings.Repeat("x", len(testMetricsToken)),
		"prefix of token":  "Bearer " + testMetricsToken[:16],
		"token, no scheme": testMetricsToken,
	} {
		t.Run(name, func(t *testing.T) {
			rec := scrape(ts, header)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Bearer") {
				t.Errorf("WWW-Authenticate = %q, want a Bearer challenge", rec.Header().Get("WWW-Authenticate"))
			}
			if strings.Contains(rec.Body.String(), "caravel_") {
				t.Error("401 body leaks metrics")
			}
		})
	}
}

func TestMetricsCountsRequestsByRoute(t *testing.T) {
	ts := newMetricsTestServer(t, testMetricsToken)
	ts.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/health", nil))

	rec := scrape(ts, "Bearer "+testMetricsToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`caravel_build_info{version=`,
		`caravel_http_requests_total{code="200",method="GET",route="/api/health"} 1`,
		`caravel_sessions_active 0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("scrape lacks %q", want)
		}
	}
}

func TestMetricsServesOpenMetricsOnRequest(t *testing.T) {
	ts := newMetricsTestServer(t, testMetricsToken)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+testMetricsToken)
	req.Header.Set("Accept", "application/openmetrics-text; version=1.0.0")
	rec := httptest.NewRecorder()
	ts.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/openmetrics-text") {
		t.Errorf("Content-Type = %q, want OpenMetrics", ct)
	}
	if !strings.HasSuffix(rec.Body.String(), "# EOF\n") {
		t.Error("OpenMetrics body does not end in # EOF")
	}
}
