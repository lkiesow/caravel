package config

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

type Config struct {
	Port      string
	DBDriver  string // "sqlite" or "postgres"
	DBDSN     string // sqlite file path, or postgres connection string
	UploadDir string
	WebDir    string // if set, serve static files live from this directory instead of the embedded copy
	// GeocoderURL is the upstream search endpoint the /api/geocode proxy
	// calls. Empty disables address search outright, and the client hides
	// the control rather than offering one that cannot work.
	GeocoderURL string

	// AI-assisted location metadata, off unless LLMURL is set.
	//
	// Env vars only, never the database, and deliberately so: a key in
	// app_settings is a key in every backup and every db dump anyone shares
	// while debugging. The same reasoning is why there is no admin screen for
	// these -- the instance owner sets them where secrets already live.
	//
	// LLMURL is an OpenAI-compatible chat-completions endpoint. The sentinel
	// value "stub" selects an in-process fake instead, which is what lets the
	// UI be built and the Playwright suite run with no key and no network.
	LLMURL   string
	LLMKey   string
	LLMModel string

	// SearchProvider names the web-search backend the assistant may call.
	// Empty means the assistant runs without web search at all, which is a
	// worse assistant but a working one -- it still has OpenStreetMap and
	// whatever the model already knows.
	SearchProvider string
	SearchKey      string
	// SearchURL is the base URL for the self-hosted providers (ddgs, searxng),
	// which have no fixed address. Ignored by the hosted ones.
	SearchURL string
}

// SearchProviders are the valid values for CARAVEL_SEARCH_PROVIDER. "stub" is
// an in-process fake for tests; the rest are real. Empty is also valid and
// means no web search.
var SearchProviders = []string{"stub", "ollama", "ddgs", "searxng", "serper"}

// LLMStub is the CARAVEL_LLM_URL sentinel selecting the in-process fake
// provider rather than a real HTTP endpoint.
const LLMStub = "stub"

// AssistEnabled reports whether the assistant is configured at all. It is the
// single off switch: everything downstream keys off this one answer rather
// than re-deriving it from a combination of fields.
func (c Config) AssistEnabled() bool { return c.LLMURL != "" }

func Load() (Config, error) {
	cfg := Config{
		Port:      getEnv("CARAVEL_PORT", "8080"),
		DBDriver:  getEnv("CARAVEL_DB_DRIVER", "sqlite"),
		DBDSN:     getEnv("CARAVEL_DB_DSN", "data/caravel.db"),
		UploadDir: getEnv("CARAVEL_UPLOAD_DIR", "uploads"),
		WebDir:    os.Getenv("CARAVEL_WEB_DIR"),
		// Nominatim is the same project the map tiles come from. It is called
		// from the server rather than the browser: OSM's usage policy wants an
		// identifying User-Agent and no more than one request a second, which
		// a browser cannot promise, and a self-hosted app should not hand a
		// user's typing to a third party without a single place to turn that
		// off.
		GeocoderURL: getEnv("CARAVEL_GEOCODER_URL", "https://nominatim.openstreetmap.org/search"),

		// No defaults on purpose. A default endpoint would mean an instance
		// that starts talking to a third party because someone set a model
		// name, which is the opposite of off-by-default.
		LLMURL:         os.Getenv("CARAVEL_LLM_URL"),
		LLMKey:         os.Getenv("CARAVEL_LLM_KEY"),
		LLMModel:       os.Getenv("CARAVEL_LLM_MODEL"),
		SearchProvider: os.Getenv("CARAVEL_SEARCH_PROVIDER"),
		SearchKey:      os.Getenv("CARAVEL_SEARCH_KEY"),
		SearchURL:      os.Getenv("CARAVEL_SEARCH_URL"),
	}

	if cfg.DBDriver != "sqlite" && cfg.DBDriver != "postgres" {
		return Config{}, fmt.Errorf("invalid CARAVEL_DB_DRIVER %q: must be %q or %q", cfg.DBDriver, "sqlite", "postgres")
	}

	// Validated at startup rather than at first use. A half-configured
	// assistant would otherwise look enabled -- the control renders, because
	// the capability is on -- and fail only when somebody presses it, which is
	// the worst moment to find out and the hardest to attribute.
	if cfg.LLMURL != "" && cfg.LLMModel == "" {
		return Config{}, fmt.Errorf("CARAVEL_LLM_URL is set but CARAVEL_LLM_MODEL is empty: both are required to enable the assistant")
	}
	if cfg.LLMModel != "" && cfg.LLMURL == "" {
		return Config{}, fmt.Errorf("CARAVEL_LLM_MODEL is set but CARAVEL_LLM_URL is empty: both are required to enable the assistant")
	}
	if cfg.SearchProvider != "" && !slices.Contains(SearchProviders, cfg.SearchProvider) {
		return Config{}, fmt.Errorf("invalid CARAVEL_SEARCH_PROVIDER %q: must be empty or one of %s", cfg.SearchProvider, strings.Join(SearchProviders, ", "))
	}
	// A search provider with no assistant to use it is a typo, not a
	// configuration: nothing else in the app searches the web.
	if cfg.SearchProvider != "" && cfg.LLMURL == "" {
		return Config{}, fmt.Errorf("CARAVEL_SEARCH_PROVIDER is set but CARAVEL_LLM_URL is empty: web search is only used by the assistant")
	}
	// The two self-hosted providers have no address anyone could guess.
	if (cfg.SearchProvider == "ddgs" || cfg.SearchProvider == "searxng") && cfg.SearchURL == "" {
		return Config{}, fmt.Errorf("CARAVEL_SEARCH_PROVIDER %q needs CARAVEL_SEARCH_URL: it is a service you run yourself", cfg.SearchProvider)
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
