package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	// Logging. Parsed here rather than in main so a typo is a startup error
	// naming the variable, which is the same rule every other setting follows.
	//
	// LogFormat is text by default because the log a self-hosted instance
	// produces is read by a person in journalctl, not by a collector. The json
	// alternative is there for anyone shipping it somewhere that parses.
	LogLevel  slog.Level // CARAVEL_LOG_LEVEL
	LogFormat string     // CARAVEL_LOG_FORMAT

	Port      string
	DBDriver  string // "sqlite" or "postgres"
	DBDSN     string // sqlite file path, or postgres connection string
	UploadDir string
	WebDir    string // if set, serve static files live from this directory instead of the embedded copy
	// GeocoderURL is the upstream search endpoint the /api/geocode proxy
	// calls. Empty disables address search outright, and the client hides
	// the control rather than offering one that cannot work.
	GeocoderURL string

	// WikimediaURL pins the Wikipedia API endpoint used by the image picker
	// and by the assistant's cover fallback. Empty -- the normal case -- means
	// each lookup goes to the edition matching the user language, which needs
	// no configuration and no key. Set it for a mirror, or to the sentinel
	// "stub" for an in-process fixture encyclopaedia (what the browser suite
	// runs against).
	WikimediaURL string

	// The map tile layer, which the browser loads directly from whoever
	// serves it -- unlike the geocoder above, which Caravel proxies. Empty
	// and zero mean "not set" and take the defaults in internal/httpapi,
	// which owns the values so they are not written down twice.
	//
	// Configurable because the default renders place names in the local
	// script: a trip to Japan is labelled 東京 rather than Tokyo, and there
	// is no language option on those tiles to change it. Swapping the
	// provider is the only fix, so the URL cannot be a literal in the
	// frontend. See docs/configuration/server.md for the providers worth
	// knowing about and what each one is good for.
	//
	// TileAttribution is HTML and is *not* escaped anywhere: the map library
	// renders it as markup, and every provider's terms require a working link
	// back.
	// It comes from the operator's own environment, the same trust level as
	// the database password sitting next to it.
	TileURL         string // CARAVEL_TILE_URL
	TileAttribution string // CARAVEL_TILE_ATTRIBUTION
	TileMaxZoom     int    // CARAVEL_TILE_MAX_ZOOM

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
	// AssistRateLimit is runs per minute per client address, and
	// AssistMaxConcurrent is how many may be in flight across the whole
	// instance at once. Zero takes the defaults in internal/httpapi. The
	// second is the one that actually bounds the worst-case bill: the first
	// is per address and does not see ten browser tabs as related.
	AssistRateLimit     int // CARAVEL_ASSIST_RATE_LIMIT
	AssistMaxConcurrent int // CARAVEL_ASSIST_MAX_CONCURRENT

	// TrustedProxies are the networks whose X-Forwarded-For header is
	// believed when deciding which address a request came from. See
	// DefaultTrustedProxies for what the default is and why.
	TrustedProxies []netip.Prefix // CARAVEL_TRUSTED_PROXIES

	// BaseURL pins the origin the instance is reached under, scheme and host,
	// with no trailing slash. It exists for exactly one job: the social
	// preview tags in the page shell need an absolute URL, and a server has no
	// reliable way to know its own public address.
	//
	// Empty -- the normal case -- means derive it per request from the scheme
	// and the Host header, which is right behind an ordinary reverse proxy.
	// Set it when something in front rewrites Host, or when the instance is
	// reached under a different name than it is addressed by.
	BaseURL string // CARAVEL_BASE_URL
}

// DefaultTrustedProxies is the private address space, which is what
// CARAVEL_TRUSTED_PROXIES takes when it is unset. The same choice Tomcat makes
// for RemoteIpValve internalProxies and Rails for trusted_proxies.
//
// It is safe because the header is read only when the *direct peer* already
// falls in one of these ranges: an instance exposed straight to the internet
// sees a public peer address, trusts nothing, and reads no header at all. What
// it buys is that the ordinary shapes -- a proxy on the same host, or one
// elsewhere on the LAN or on a container network -- need no configuration.
//
// The cost is local and real: whoever can already reach the server from a
// private address can forge X-Forwarded-For and pick the address the rate
// limiters key on. An operator who cares narrows this to their proxy, or sets
// the value to "none".
//
// 100.64.0.0/10 is deliberately absent although Rails includes it. That is
// Tailscale's range, and on a tailnet those addresses are usually end users
// rather than proxies, so trusting them by default would hand every peer the
// forgery above.
var DefaultTrustedProxies = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fc00::/7"),
}

// TrustedProxiesNone is the CARAVEL_TRUSTED_PROXIES sentinel for an empty set.
// Needed because an unset variable already means the default, so there has to
// be a way to spell "trust nothing".
const TrustedProxiesNone = "none"

// SearchProviders are the valid values for CARAVEL_SEARCH_PROVIDER. "stub" is
// an in-process fake for tests; the rest are real. Empty is also valid and
// means no web search.
var SearchProviders = []string{"stub", "ollama", "ddgs", "serper"}

// LLMStub is the CARAVEL_LLM_URL sentinel selecting the in-process fake
// provider rather than a real HTTP endpoint.
const LLMStub = "stub"

// maxTileZoom is the deepest zoom the XYZ tile scheme addresses at all. No
// provider serves this far down -- the usual ceiling is 19 or 20 -- but a
// number beyond it is certainly a typo rather than an ambitious operator.
const maxTileZoom = 22

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
		// Trailing slash trimmed here rather than at every use: the tags
		// concatenate it with paths that start with one.
		BaseURL: strings.TrimRight(strings.TrimSpace(os.Getenv("CARAVEL_BASE_URL")), "/"),
		// Nominatim is the same project the map tiles come from. It is called
		// from the server rather than the browser: OSM's usage policy wants an
		// identifying User-Agent and no more than one request a second, which
		// a browser cannot promise, and a self-hosted app should not hand a
		// user's typing to a third party without a single place to turn that
		// off.
		GeocoderURL:  getEnv("CARAVEL_GEOCODER_URL", "https://nominatim.openstreetmap.org/search"),
		WikimediaURL: os.Getenv("CARAVEL_WIKIMEDIA_URL"),

		// No defaults here: internal/httpapi holds them, the same way it
		// holds the assist limiter defaults.
		TileURL:         strings.TrimSpace(os.Getenv("CARAVEL_TILE_URL")),
		TileAttribution: strings.TrimSpace(os.Getenv("CARAVEL_TILE_ATTRIBUTION")),

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
	cfg.AssistMaxConcurrent = pickInt("CARAVEL_ASSIST_MAX_CONCURRENT")
	cfg.TileMaxZoom = pickInt("CARAVEL_TILE_MAX_ZOOM")

	proxies, err := getEnvPrefixList("CARAVEL_TRUSTED_PROXIES", DefaultTrustedProxies)
	if err != nil {
		errs = append(errs, err)
	}
	cfg.TrustedProxies = proxies

	level, err := parseLogLevel(os.Getenv("CARAVEL_LOG_LEVEL"))
	if err != nil {
		errs = append(errs, err)
	}
	cfg.LogLevel = level
	format, err := parseLogFormat(os.Getenv("CARAVEL_LOG_FORMAT"))
	if err != nil {
		errs = append(errs, err)
	}
	cfg.LogFormat = format

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
	// A search provider used to require the assistant, on the reasoning that
	// nothing else in the app searched the web. Stage 21 Milestone 7 made that
	// false: the "Search for an image" control in the location editor uses the
	// same backend and has no LLM in it at all, so a provider configured on
	// its own is now a working configuration rather than a typo.
	// ddgs is a service the operator runs, so it has no address anyone could
	// guess. The hosted providers default to their own endpoint and only use
	// CARAVEL_SEARCH_URL as an override.
	if cfg.SearchProvider == "ddgs" && cfg.SearchURL == "" {
		return Config{}, fmt.Errorf("CARAVEL_SEARCH_PROVIDER %q needs CARAVEL_SEARCH_URL: it is a service you run yourself", cfg.SearchProvider)
	}

	// Checked at startup because the failure is otherwise invisible: a
	// mistyped base URL does not break the app, it quietly produces a social
	// card nobody can fetch, which is only ever noticed by whoever pastes a
	// link somewhere public.
	if cfg.BaseURL != "" {
		u, err := url.Parse(cfg.BaseURL)
		if err != nil {
			return Config{}, fmt.Errorf("invalid CARAVEL_BASE_URL %q: %w", cfg.BaseURL, err)
		}
		if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return Config{}, fmt.Errorf("invalid CARAVEL_BASE_URL %q: needs an absolute http or https URL, like https://caravel.example", cfg.BaseURL)
		}
		if u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return Config{}, fmt.Errorf("invalid CARAVEL_BASE_URL %q: scheme and host only, with no path", cfg.BaseURL)
		}
	}

	// A tile URL without its placeholders is not a slow map, it is a blank
	// one: the browser requests a literal "{z}" once, gets a 404, and shows
	// grey squares with nothing in the UI pointing at the variable that
	// caused it. Checked here so the server refuses to start instead.
	if cfg.TileURL != "" {
		for _, placeholder := range []string{"{z}", "{x}", "{y}"} {
			if !strings.Contains(cfg.TileURL, placeholder) {
				return Config{}, fmt.Errorf("invalid CARAVEL_TILE_URL %q: missing %s -- the template needs {z}, {x} and {y}", cfg.TileURL, placeholder)
			}
		}
	}
	// getEnvInt has already refused a negative or unparseable value; 0 means
	// unset. The ceiling is the deepest zoom the XYZ scheme defines, and the
	// floor rules out a 0 that would be indistinguishable from unset anyway.
	if cfg.TileMaxZoom > maxTileZoom {
		return Config{}, fmt.Errorf("invalid CARAVEL_TILE_MAX_ZOOM %d: must be between 1 and %d", cfg.TileMaxZoom, maxTileZoom)
	}

	return cfg, nil
}

// LogFormats are the valid values for CARAVEL_LOG_FORMAT.
var LogFormats = []string{"text", "json"}

// logLevels maps what an operator writes to what slog uses. Only these four,
// spelled in lower case: slog understands "DEBUG+2" and similar, and accepting
// that here would mean documenting a syntax nobody wants and supporting a
// level with no name.
var logLevels = map[string]slog.Level{
	"debug": slog.LevelDebug,
	"info":  slog.LevelInfo,
	"warn":  slog.LevelWarn,
	"error": slog.LevelError,
}

// parseLogLevel reads CARAVEL_LOG_LEVEL. Empty means info.
//
// An unrecognised value is an error rather than a fall back to info, for the
// reason that applies to every setting here: somebody who wrote "verbose" and
// got silence would conclude the flag does nothing, which is a worse outcome
// than a server that refuses to start and says what the four words are.
func parseLogLevel(raw string) (slog.Level, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" {
		return slog.LevelInfo, nil
	}
	level, ok := logLevels[name]
	if !ok {
		return 0, fmt.Errorf("invalid CARAVEL_LOG_LEVEL %q: must be empty or one of debug, info, warn, error", raw)
	}
	return level, nil
}

// parseLogFormat reads CARAVEL_LOG_FORMAT. Empty means text.
func parseLogFormat(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if name == "" {
		return "text", nil
	}
	if !slices.Contains(LogFormats, name) {
		return "", fmt.Errorf("invalid CARAVEL_LOG_FORMAT %q: must be empty or one of %s", raw, strings.Join(LogFormats, ", "))
	}
	return name, nil
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

// getEnvPrefixList reads a comma-separated list of CIDR prefixes and bare
// addresses, returning fallback when unset and an empty list for the sentinel
// "none". A bare address becomes a single-host prefix. Setting the variable
// replaces the fallback rather than extending it -- an operator naming their
// own proxy should get that proxy and not also the defaults.
func getEnvPrefixList(key string, fallback []netip.Prefix) ([]netip.Prefix, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	if strings.EqualFold(raw, TrustedProxiesNone) {
		return nil, nil
	}
	var out []netip.Prefix
	for _, field := range strings.Split(raw, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if p, err := netip.ParsePrefix(field); err == nil {
			out = append(out, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(field)
		if err != nil {
			return nil, fmt.Errorf("invalid %s %q: %q is neither an address nor a CIDR range", key, raw, field)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("invalid %s %q: no addresses or ranges in it; use %q to trust none", key, raw, TrustedProxiesNone)
	}
	return out, nil
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
