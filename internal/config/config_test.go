package config

import (
	"strings"
	"testing"
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
			// Nothing else in the app searches the web, so this is a typo
			// rather than a configuration.
			name:    "a search provider without an assistant is refused",
			env:     map[string]string{"CARAVEL_SEARCH_PROVIDER": "serper"},
			wantErr: "web search is only used by the assistant",
		},
		{
			name: "the self-hosted providers need an address",
			env: map[string]string{
				"CARAVEL_LLM_URL": "stub", "CARAVEL_LLM_MODEL": "stub",
				"CARAVEL_SEARCH_PROVIDER": "searxng",
			},
			wantErr: "needs CARAVEL_SEARCH_URL",
		},
		{
			name: "ddgs likewise",
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
