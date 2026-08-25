package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

// Load reads the process environment, so each case sets exactly the vars it
// cares about and t.Setenv restores them. The assistant vars have no defaults,
// so an unset var really is off.
func TestLoadAssistValidation(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		wantErr string // substring; empty means the config must load
		check   func(*testing.T, Config)
	}{
		{
			name: "unset is off, and not an error",
			env:  map[string]string{},
			check: func(t *testing.T, c Config) {
				if c.AssistEnabled() {
					t.Error("AssistEnabled() = true with nothing configured")
				}
			},
		},
		{
			name: "url and model together enable it",
			env:  map[string]string{"CARAVEL_LLM_URL": "https://example.invalid/v1", "CARAVEL_LLM_MODEL": "some-model"},
			check: func(t *testing.T, c Config) {
				if !c.AssistEnabled() {
					t.Error("AssistEnabled() = false with url and model set")
				}
			},
		},
		{
			name: "the stub sentinel is a valid url",
			env:  map[string]string{"CARAVEL_LLM_URL": "stub", "CARAVEL_LLM_MODEL": "stub"},
			check: func(t *testing.T, c Config) {
				if c.LLMURL != LLMStub {
					t.Errorf("LLMURL = %q, want %q", c.LLMURL, LLMStub)
				}
			},
		},
		{
			// The half-configured cases are the point of validating at
			// startup: without this the capability reads as on and the
			// feature fails only when somebody presses the button.
			name:    "url without model is refused",
			env:     map[string]string{"CARAVEL_LLM_URL": "https://example.invalid/v1"},
			wantErr: "CARAVEL_LLM_MODEL is empty",
		},
		{
			name:    "model without url is refused",
			env:     map[string]string{"CARAVEL_LLM_MODEL": "some-model"},
			wantErr: "CARAVEL_LLM_URL is empty",
		},
		{
			name: "a known search provider is accepted",
			env: map[string]string{
				"CARAVEL_LLM_URL": "stub", "CARAVEL_LLM_MODEL": "stub",
				"CARAVEL_SEARCH_PROVIDER": "serper", "CARAVEL_SEARCH_KEY": "k",
			},
			check: func(t *testing.T, c Config) {
				if c.SearchProvider != "serper" {
					t.Errorf("SearchProvider = %q", c.SearchProvider)
				}
			},
		},
		{
			name: "an unknown search provider names the valid ones",
			env: map[string]string{
				"CARAVEL_LLM_URL": "stub", "CARAVEL_LLM_MODEL": "stub",
				"CARAVEL_SEARCH_PROVIDER": "altavista",
			},
			wantErr: "must be empty or one of",
		},
		{
			// It used to be refused: nothing else searched the web, so it read
			// as a typo. Stage 21 Milestone 7 gave the image picker the same
			// backend, so this is now an instance with image search and no
			// assistant -- a combination somebody might deliberately want.
			name: "a search provider without an assistant is now accepted",
			env: map[string]string{
				"CARAVEL_SEARCH_PROVIDER": "serper", "CARAVEL_SEARCH_KEY": "k",
			},
			check: func(t *testing.T, c Config) {
				if c.SearchProvider != "serper" || c.LLMURL != "" {
					t.Errorf("SearchProvider = %q, LLMURL = %q", c.SearchProvider, c.LLMURL)
				}
			},
		},
		{
			// Dropped in Milestone 8 along with the searxng implementation:
			// see the backlog entry. A name that is no longer supported must
			// be refused rather than silently ignored.
			name: "a provider that was deferred is not accepted",
			env: map[string]string{
				"CARAVEL_LLM_URL": "stub", "CARAVEL_LLM_MODEL": "stub",
				"CARAVEL_SEARCH_PROVIDER": "searxng",
			},
			wantErr: "must be empty or one of",
		},
		{
			name: "ddgs, which is self-hosted, needs an address",
			env: map[string]string{
				"CARAVEL_LLM_URL": "stub", "CARAVEL_LLM_MODEL": "stub",
				"CARAVEL_SEARCH_PROVIDER": "ddgs",
			},
			wantErr: "needs CARAVEL_SEARCH_URL",
		},
		{
			name: "ddgs with an address is fine",
			env: map[string]string{
				"CARAVEL_LLM_URL": "stub", "CARAVEL_LLM_MODEL": "stub",
				"CARAVEL_SEARCH_PROVIDER": "ddgs", "CARAVEL_SEARCH_URL": "http://localhost:4479",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Cleared explicitly rather than trusting the ambient
			// environment: a developer with CARAVEL_LLM_URL exported would
			// otherwise see different results from CI.
			for _, k := range []string{
				"CARAVEL_LLM_URL", "CARAVEL_LLM_KEY", "CARAVEL_LLM_MODEL",
				"CARAVEL_SEARCH_PROVIDER", "CARAVEL_SEARCH_KEY", "CARAVEL_SEARCH_URL",
			} {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			cfg, err := Load()
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("Load() succeeded, want error containing %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Load() error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() = %v, want success", err)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}

// The guard-rail overrides. A typo here is the kind of thing that silently
// does nothing for a week, so an unparseable value is a startup error naming
// the variable rather than a quiet fallback to the default.
func TestLoadAssistLimits(t *testing.T) {
	limitVars := []string{
		"CARAVEL_ASSIST_TIMEOUT", "CARAVEL_ASSIST_ANSWER_TIMEOUT",
		"CARAVEL_ASSIST_MAX_TURNS", "CARAVEL_ASSIST_MAX_TOOL_CALLS",
		"CARAVEL_ASSIST_MAX_TOKENS", "CARAVEL_ASSIST_ANSWER_RESERVE",
		"CARAVEL_ASSIST_RATE_LIMIT",
	}
	clear := func(t *testing.T) {
		for _, k := range limitVars {
			t.Setenv(k, "")
		}
	}

	t.Run("unset leaves zero, which means use the default", func(t *testing.T) {
		clear(t)
		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.AssistMaxTokens != 0 || cfg.AssistTimeout != 0 || cfg.AssistRateLimit != 0 {
			t.Errorf("unset limits are not zero: %+v", cfg)
		}
	})

	t.Run("values are parsed", func(t *testing.T) {
		clear(t)
		t.Setenv("CARAVEL_ASSIST_TIMEOUT", "90s")
		t.Setenv("CARAVEL_ASSIST_ANSWER_TIMEOUT", "2m")
		t.Setenv("CARAVEL_ASSIST_MAX_TURNS", "5")
		t.Setenv("CARAVEL_ASSIST_MAX_TOOL_CALLS", "8")
		t.Setenv("CARAVEL_ASSIST_MAX_TOKENS", "50000")
		t.Setenv("CARAVEL_ASSIST_ANSWER_RESERVE", "8000")
		t.Setenv("CARAVEL_ASSIST_RATE_LIMIT", "3")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.AssistTimeout != 90*time.Second {
			t.Errorf("AssistTimeout = %v", cfg.AssistTimeout)
		}
		if cfg.AssistAnswerTimeout != 2*time.Minute {
			t.Errorf("AssistAnswerTimeout = %v", cfg.AssistAnswerTimeout)
		}
		if cfg.AssistMaxTurns != 5 || cfg.AssistMaxToolCalls != 8 {
			t.Errorf("turns/tools = %d/%d", cfg.AssistMaxTurns, cfg.AssistMaxToolCalls)
		}
		if cfg.AssistMaxTokens != 50000 || cfg.AssistAnswerReserve != 8000 {
			t.Errorf("tokens/reserve = %d/%d", cfg.AssistMaxTokens, cfg.AssistAnswerReserve)
		}
		if cfg.AssistRateLimit != 3 {
			t.Errorf("AssistRateLimit = %d", cfg.AssistRateLimit)
		}
	})

	t.Run("a bad value names its variable", func(t *testing.T) {
		for _, tc := range []struct{ key, value, want string }{
			// The letter O for a zero: the exact typo that would otherwise
			// look like the setting simply had no effect.
			{"CARAVEL_ASSIST_MAX_TOKENS", "12O000", "CARAVEL_ASSIST_MAX_TOKENS"},
			{"CARAVEL_ASSIST_MAX_TURNS", "lots", "CARAVEL_ASSIST_MAX_TURNS"},
			{"CARAVEL_ASSIST_MAX_TURNS", "-1", "must not be negative"},
			{"CARAVEL_ASSIST_TIMEOUT", "2 minutes", "CARAVEL_ASSIST_TIMEOUT"},
			{"CARAVEL_ASSIST_TIMEOUT", "120", "duration such as"},
		} {
			t.Run(tc.key+"="+tc.value, func(t *testing.T) {
				clear(t)
				t.Setenv(tc.key, tc.value)
				_, err := Load()
				if err == nil {
					t.Fatalf("Load accepted %s=%q", tc.key, tc.value)
				}
				if !strings.Contains(err.Error(), tc.want) {
					t.Errorf("error = %v, want it to mention %q", err, tc.want)
				}
			})
		}
	})

	t.Run("several bad values are all reported", func(t *testing.T) {
		// Fixing one typo, restarting, and finding another is a poor way to
		// spend an afternoon.
		clear(t)
		t.Setenv("CARAVEL_ASSIST_MAX_TURNS", "nope")
		t.Setenv("CARAVEL_ASSIST_TIMEOUT", "also nope")
		_, err := Load()
		if err == nil {
			t.Fatal("Load accepted two bad values")
		}
		if !strings.Contains(err.Error(), "MAX_TURNS") || !strings.Contains(err.Error(), "TIMEOUT") {
			t.Errorf("error = %v, want both variables named", err)
		}
	})
}

// The tile vars carry no defaults of their own -- internal/httpapi owns those
// -- so what Load has to get right is passing a set value through untouched
// and refusing one that would produce a blank map with no visible cause.
func TestLoadTileValidation(t *testing.T) {
	const carto = "https://basemaps.cartocdn.com/rastertiles/voyager/{z}/{x}/{y}{r}.png"

	cases := []struct {
		name    string
		env     map[string]string
		wantErr string // substring; empty means the config must load
		check   func(*testing.T, Config)
	}{
		{
			name: "unset leaves the defaults to httpapi",
			env:  map[string]string{},
			check: func(t *testing.T, c Config) {
				if c.TileURL != "" || c.TileAttribution != "" || c.TileMaxZoom != 0 {
					t.Errorf("unset tile vars produced %q / %q / %d, want all zero", c.TileURL, c.TileAttribution, c.TileMaxZoom)
				}
			},
		},
		{
			name: "a set provider comes through verbatim",
			env: map[string]string{
				"CARAVEL_TILE_URL":         carto,
				"CARAVEL_TILE_ATTRIBUTION": "&copy; CARTO",
				"CARAVEL_TILE_MAX_ZOOM":    "20",
			},
			check: func(t *testing.T, c Config) {
				if c.TileURL != carto {
					t.Errorf("TileURL = %q, want %q", c.TileURL, carto)
				}
				if c.TileAttribution != "&copy; CARTO" {
					t.Errorf("TileAttribution = %q, want the configured markup unescaped", c.TileAttribution)
				}
				if c.TileMaxZoom != 20 {
					t.Errorf("TileMaxZoom = %d, want 20", c.TileMaxZoom)
				}
			},
		},
		{
			// Each placeholder separately: a URL with {z} and {x} but no {y}
			// still fetches something, so the check cannot stop at the first.
			name:    "a url missing the y placeholder is refused",
			env:     map[string]string{"CARAVEL_TILE_URL": "https://tiles.invalid/{z}/{x}.png"},
			wantErr: "missing {y}",
		},
		{
			name:    "a url with no placeholders at all is refused",
			env:     map[string]string{"CARAVEL_TILE_URL": "https://tiles.invalid/tile.png"},
			wantErr: "missing {z}",
		},
		{
			name:    "a zoom past the scheme is refused",
			env:     map[string]string{"CARAVEL_TILE_MAX_ZOOM": "30"},
			wantErr: "CARAVEL_TILE_MAX_ZOOM 30",
		},
		{
			name:    "a negative zoom is refused by getEnvInt",
			env:     map[string]string{"CARAVEL_TILE_MAX_ZOOM": "-1"},
			wantErr: "must not be negative",
		},
		{
			name:    "a misspelled zoom is refused rather than ignored",
			env:     map[string]string{"CARAVEL_TILE_MAX_ZOOM": "2O"},
			wantErr: "must be a whole number",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{"CARAVEL_TILE_URL", "CARAVEL_TILE_ATTRIBUTION", "CARAVEL_TILE_MAX_ZOOM"} {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			cfg, err := Load()
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("Load() succeeded, want error containing %q", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Load() error = %v, want it to contain %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() = %v, want success", err)
			}
			if tc.check != nil {
				tc.check(t, cfg)
			}
		})
	}
}

// Logging. Both settings follow the same rule as every other one here: a value
// nobody recognises is a startup error naming the variable, not a silent fall
// back to the default. Somebody who wrote "verbose" and got info would
// conclude the flag does nothing.
func TestLogLevelAndFormat(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		want    slog.Level
		format  string
		wantErr string
	}{
		{name: "unset defaults to info", want: slog.LevelInfo, format: "text"},
		{name: "debug", env: map[string]string{"CARAVEL_LOG_LEVEL": "debug"}, want: slog.LevelDebug, format: "text"},
		{name: "case and space are forgiven", env: map[string]string{"CARAVEL_LOG_LEVEL": " WARN "}, want: slog.LevelWarn, format: "text"},
		{name: "error", env: map[string]string{"CARAVEL_LOG_LEVEL": "error"}, want: slog.LevelError, format: "text"},
		{name: "json format", env: map[string]string{"CARAVEL_LOG_FORMAT": "json"}, want: slog.LevelInfo, format: "json"},
		{
			name:    "an unknown level names the four that work",
			env:     map[string]string{"CARAVEL_LOG_LEVEL": "verbose"},
			wantErr: "CARAVEL_LOG_LEVEL",
		},
		{
			// slog itself accepts this; the app does not, because documenting
			// that syntax buys a level with no name.
			name:    "an offset level is refused",
			env:     map[string]string{"CARAVEL_LOG_LEVEL": "DEBUG+2"},
			wantErr: "CARAVEL_LOG_LEVEL",
		},
		{
			name:    "an unknown format is refused",
			env:     map[string]string{"CARAVEL_LOG_FORMAT": "logfmt"},
			wantErr: "CARAVEL_LOG_FORMAT",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			cfg, err := Load()
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("Load() accepted %v", tc.env)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error = %v, want it to name %s", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.LogLevel != tc.want {
				t.Errorf("LogLevel = %v, want %v", cfg.LogLevel, tc.want)
			}
			if cfg.LogFormat != tc.format {
				t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, tc.format)
			}
		})
	}
}
