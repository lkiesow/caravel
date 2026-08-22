package config

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
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
	// LLMURL is an OpenAI-compatible endpoint: either the base URL the
	// provider documents ("https://openrouter.ai/api/v1") or the full
	// chat-completions path. The sentinel value "stub" selects an in-process
	// fake instead, which is what lets the UI be built and the Playwright
	// suite run with no key and no network.
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

	// Guard rails on one assistant run, and on how often runs may be started.
	//
	// Every one of these is settable because they are the numbers an operator
	// needs to change *fast*: a chattier model, a search backend returning
	// fatter extracts, or a bill larger than expected are all reasons to turn
	// one of these today rather than at the next release. Zero means "not
	// set" and takes the shipped default -- see assist.DefaultLimits, which
	// owns the values so they are not written down twice.
	//
	// Note what these do and do not bound. The first five bound *one run*.
	// AssistRateLimit is the only thing bounding how many runs happen, so the
	// worst-case spend for an instance is roughly the two multiplied
	// together, per client address.
	AssistTimeout       time.Duration // CARAVEL_ASSIST_TIMEOUT, e.g. "2m"
	AssistAnswerTimeout time.Duration // CARAVEL_ASSIST_ANSWER_TIMEOUT
	AssistMaxTurns      int           // CARAVEL_ASSIST_MAX_TURNS
	AssistMaxToolCalls  int           // CARAVEL_ASSIST_MAX_TOOL_CALLS
	AssistMaxTokens     int           // CARAVEL_ASSIST_MAX_TOKENS
	AssistAnswerReserve int           // CARAVEL_ASSIST_ANSWER_RESERVE
	// AssistRateLimit is runs per minute per client address. Zero takes the
	// default in internal/httpapi.
	AssistRateLimit int // CARAVEL_ASSIST_RATE_LIMIT
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

	// Parsed after the struct literal because each may fail, and a typo in a
	// limit should stop the server with the variable named rather than fall
	// back to a default the operator did not ask for. Silently ignoring
	// "CARAVEL_ASSIST_MAX_TOKENS=12O000" (letter O) is how somebody spends a
	// week wondering why their change did nothing.
	var errs []error
	pickDuration := func(key string) time.Duration {
		v, err := getEnvDuration(key)
		if err != nil {
			errs = append(errs, err)
		}
		return v
	}
	pickInt := func(key string) int {
		v, err := getEnvInt(key)
		if err != nil {
			errs = append(errs, err)
		}
		return v
	}
	cfg.AssistTimeout = pickDuration("CARAVEL_ASSIST_TIMEOUT")
	cfg.AssistAnswerTimeout = pickDuration("CARAVEL_ASSIST_ANSWER_TIMEOUT")
	cfg.AssistMaxTurns = pickInt("CARAVEL_ASSIST_MAX_TURNS")
	cfg.AssistMaxToolCalls = pickInt("CARAVEL_ASSIST_MAX_TOOL_CALLS")
	cfg.AssistMaxTokens = pickInt("CARAVEL_ASSIST_MAX_TOKENS")
	cfg.AssistAnswerReserve = pickInt("CARAVEL_ASSIST_ANSWER_RESERVE")
	cfg.AssistRateLimit = pickInt("CARAVEL_ASSIST_RATE_LIMIT")
	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
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

// getEnvInt reads a non-negative integer, or 0 when unset. A negative or
// unparseable value is an error rather than a fallback: it is a typo, and the
// operator should hear about it now.
func getEnvInt(key string) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: must be a whole number", key, raw)
	}
	if n < 0 {
		return 0, fmt.Errorf("invalid %s %q: must not be negative", key, raw)
	}
	return n, nil
}

// getEnvDuration reads a Go duration ("90s", "2m"), or 0 when unset.
func getEnvDuration(key string) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: must be a duration such as 90s or 2m", key, raw)
	}
	if d < 0 {
		return 0, fmt.Errorf("invalid %s %q: must not be negative", key, raw)
	}
	return d, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
