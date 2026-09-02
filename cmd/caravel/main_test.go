package main

import (
	"net/netip"
	"testing"
	"testing/fstest"

	"caravel/internal/config"
	"caravel/internal/httpapi"
)

// The gap these tests close is narrow and was expensive: internal/httpapi
// proves at length that an Options value is honoured -- TestAssistIsRateLimited
// and TestAssistRefusesWhenAllSlotsAreBusy both build a server from Options and
// watch the limits bite -- and internal/config proves the environment is parsed
// into a Config. Nothing proved the step between them, so two settings went
// missing for six stages while both halves stayed green.

func TestServerOptionsCarriesTheAssistLimits(t *testing.T) {
	// Deliberately values that are neither the defaults nor zero: passing the
	// default would make a dropped field indistinguishable from a set one.
	cfg := config.Config{AssistRateLimit: 11, AssistMaxConcurrent: 3}

	opts := serverOptions(cfg, httpapi.Options{})

	if opts.AssistRateLimit != 11 {
		t.Errorf("AssistRateLimit = %d, want 11", opts.AssistRateLimit)
	}
	if opts.AssistMaxConcurrent != 3 {
		t.Errorf("AssistMaxConcurrent = %d, want 3", opts.AssistMaxConcurrent)
	}
	if opts.AssistRateLimit == httpapi.DefaultAssistRateLimit {
		t.Error("AssistRateLimit happens to equal the default, which would make this test unable to fail")
	}
	if opts.AssistMaxConcurrent == httpapi.DefaultAssistMaxConcurrent {
		t.Error("AssistMaxConcurrent happens to equal the default, which would make this test unable to fail")
	}
}

// An unset limit stays zero rather than being defaulted here, because NewServer
// is what turns zero into the default (assistRateLimit / assistMaxConcurrent).
// Defaulting in both places would work today and diverge the day one of them
// changes.
func TestServerOptionsLeavesUnsetAssistLimitsAtZero(t *testing.T) {
	opts := serverOptions(config.Config{}, httpapi.Options{})

	if opts.AssistRateLimit != 0 || opts.AssistMaxConcurrent != 0 {
		t.Errorf("unset limits = %d/%d, want 0/0 so NewServer applies the defaults",
			opts.AssistRateLimit, opts.AssistMaxConcurrent)
	}
}

func TestServerOptionsCarriesTheRestOfTheConfiguration(t *testing.T) {
	proxies := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	cfg := config.Config{
		WebDir:          "web",
		TrustedProxies:  proxies,
		BaseURL:         "https://caravel.example",
		MapStyleURL:     "https://tiles.example/styles/day",
		MapStyleDarkURL: "https://tiles.example/styles/night",
	}

	opts := serverOptions(cfg, httpapi.Options{})

	// NoCache is derived, not copied: serving assets live from disk is the
	// whole reason not to let the browser keep them.
	if !opts.NoCache {
		t.Error("NoCache = false with CARAVEL_WEB_DIR set, want true")
	}
	if len(opts.TrustedProxies) != 1 || opts.TrustedProxies[0] != proxies[0] {
		t.Errorf("TrustedProxies = %v, want %v", opts.TrustedProxies, proxies)
	}
	if opts.BaseURL != cfg.BaseURL {
		t.Errorf("BaseURL = %q, want %q", opts.BaseURL, cfg.BaseURL)
	}
	if opts.MapStyle.URL != cfg.MapStyleURL {
		t.Errorf("MapStyle.URL = %q, want %q", opts.MapStyle.URL, cfg.MapStyleURL)
	}
	if opts.MapStyle.DarkURL != cfg.MapStyleDarkURL {
		t.Errorf("MapStyle.DarkURL = %q, want %q", opts.MapStyle.DarkURL, cfg.MapStyleDarkURL)
	}
}

// The embedded-assets case, which is what a released instance runs.
func TestServerOptionsWithoutAWebDirLetsTheBrowserCache(t *testing.T) {
	if serverOptions(config.Config{}, httpapi.Options{}).NoCache {
		t.Error("NoCache = true with no CARAVEL_WEB_DIR, want false")
	}
}

// serverOptions only fills in the configured half; it must not disturb the
// collaborators main has already constructed and put in the same value.
func TestServerOptionsLeavesTheCollaboratorsAlone(t *testing.T) {
	webFS := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("<!doctype html>")}}

	opts := serverOptions(config.Config{}, httpapi.Options{WebFS: webFS})

	if opts.WebFS == nil {
		t.Fatal("WebFS was dropped")
	}
	if _, err := opts.WebFS.Open("index.html"); err != nil {
		t.Errorf("WebFS no longer serves the file it was given: %v", err)
	}
}
