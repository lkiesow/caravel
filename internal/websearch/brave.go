package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// Brave Search API (api-dashboard.search.brave.com), hosted.
//
// An independent index behind a real API, like Serper in every way that
// matters to an operator -- a key, no service to run, no scraping -- and
// unlike it in price: a monthly credit that a personal instance rarely uses
// up, where Serper sells packs that expire. That trade is the whole reason it
// is here; Stage 16 left it out only because it would have occupied Serper's
// slot.
//
// GET with the key in an X-Subscription-Token header and the query in the URL,
// rather than the JSON POST the other hosted backends take. The web endpoint
// answers {"web": {"results": [{"title", "url", "description"}]}} -- a fourth
// spelling of the same three things.
//
// The field names below were taken from live responses (Stage 46), and one
// thing in them is not in the documentation's examples: `description`, and
// sometimes `title`, carry HTML -- <strong> around the matched words and
// entities like &amp;. Handed to the model as is, that is markup it has to
// read past; shown in the image picker, it is literal tags. So every text
// field goes through stripHTML.

const braveSearchURL = "https://api.search.brave.com/res/v1/web/search"

type braveSearcher struct {
	url string
	// imageURL is the /images/search sibling of url, derived rather than
	// configured for the same reason as Serper's: one override covers both.
	imageURL string
	key      string
	client   *http.Client
}

func newBraveSearcher(key, overrideURL string) *braveSearcher {
	endpoint := braveSearchURL
	if strings.TrimSpace(overrideURL) != "" {
		endpoint = strings.TrimSpace(overrideURL)
	}
	base := strings.TrimSuffix(endpoint, "/web/search")
	return &braveSearcher{
		url:      endpoint,
		imageURL: base + "/images/search",
		key:      key,
		client:   &http.Client{Timeout: searchTimeout},
	}
}

func (*braveSearcher) Name() string { return "brave" }

func (s *braveSearcher) Search(ctx context.Context, query string) ([]Result, error) {
	raw, err := s.get(ctx, "search", s.url, url.Values{
		"q":     {query},
		"count": {fmt.Sprint(MaxResults)},
	})
	if err != nil {
		return nil, err
	}

	var decoded struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("the search service returned a response that could not be read: %w", err)
	}

	out := make([]Result, 0, len(decoded.Web.Results))
	for _, r := range decoded.Web.Results {
		if strings.TrimSpace(r.URL) == "" {
			continue
		}
		out = append(out, Result{
			Title:   collapseWhitespace(stripHTML(r.Title)),
			URL:     strings.TrimSpace(r.URL),
			Snippet: truncate(collapseWhitespace(stripHTML(r.Description)), 600),
		})
	}
	return out, nil
}

// SearchImages implements ImageSearcher.
//
// GET /images/search, answering a `results` array in which the picture and the
// page are in different places:
//
//	{"title": "...", "url": "https://hostelgeeks.com/kex-hostel-...",
//	 "thumbnail": {"src": "https://imgs.search.brave.com/...", "width": 500, "height": 313},
//	 "properties": {"url": "https://hostelgeeks.com/wp-content/...jpg", "width": 900, "height": 563}}
//
// `url` is the page and `properties.url` is the picture -- the same pair that
// is easy to get the wrong way round with Serper, spelled differently. The
// thumbnail is Brave's own proxy, resized to 500px wide, which is the better
// half of this backend for the picker: a proxied thumbnail is not
// hotlink-blocked by the site it came from. All twelve in the live sample
// loaded with a foreign Referer.
func (s *braveSearcher) SearchImages(ctx context.Context, query string) ([]ImageResult, error) {
	raw, err := s.get(ctx, "image search", s.imageURL, url.Values{
		"q":     {query},
		"count": {fmt.Sprint(imageSearchMaxResults)},
	})
	if err != nil {
		return nil, err
	}

	var decoded struct {
		Results []struct {
			Title     string `json:"title"`
			URL       string `json:"url"`
			Thumbnail struct {
				Src string `json:"src"`
			} `json:"thumbnail"`
			Properties struct {
				URL    string `json:"url"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"properties"`
		} `json:"results"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, fmt.Errorf("the image search service returned a response that could not be read: %w", err)
	}

	out := make([]ImageResult, 0, len(decoded.Results))
	for _, r := range decoded.Results {
		if strings.TrimSpace(r.Properties.URL) == "" {
			continue
		}
		out = append(out, ImageResult{
			Title:     truncate(collapseWhitespace(stripHTML(r.Title)), 200),
			URL:       strings.TrimSpace(r.Properties.URL),
			ThumbURL:  strings.TrimSpace(r.Thumbnail.Src),
			Width:     r.Properties.Width,
			Height:    r.Properties.Height,
			SourceURL: strings.TrimSpace(r.URL),
		})
	}
	return out, nil
}

// get makes one authenticated GET and returns the body of a 200.
//
// Shared by every endpoint, unlike Serper's copies of the same, because here
// the requests really are identical apart from the URL: no body to build, the
// same header, the same status codes. `what` names the service in errors
// ("search", "image search"), so a log line still says which half failed.
func (s *braveSearcher) get(ctx context.Context, what, endpoint string, params url.Values) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, searchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", s.key)
	req.Header.Set("User-Agent", userAgent())

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the %s service could not be reached: %w", what, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		// Out of credit and rate limited are named on their own, for the same
		// reason as Serper's 402: "the key was refused" would send an operator
		// to check a key that is perfectly fine. Brave meters per second as
		// well as per month, so 429 is a real answer and not a theoretical one.
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			return nil, fmt.Errorf("the %s service rejected the API key (status %d)", what, resp.StatusCode)
		case http.StatusPaymentRequired:
			return nil, fmt.Errorf("the %s service reports the account is out of credit (status 402)", what)
		case http.StatusTooManyRequests:
			return nil, fmt.Errorf("the %s service is rate limiting this key (status 429)", what)
		}
		return nil, fmt.Errorf("the %s service responded with status %d", what, resp.StatusCode)
	}
	return raw, nil
}

// htmlTag matches one tag. Deliberately crude: what Brave puts in a snippet is
// emphasis around matched words, not documents, and the point is plain text
// rather than a faithful rendering.
var htmlTag = regexp.MustCompile(`<[^>]*>`)

// stripHTML turns Brave's highlighted text into plain text. Tags go first and
// entities second, so an escaped "&lt;b&gt;" in the source survives as the
// literal text it was rather than being stripped as a tag.
func stripHTML(s string) string {
	return html.UnescapeString(htmlTag.ReplaceAllString(s, ""))
}
