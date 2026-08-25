// Package wikimedia looks up a Wikipedia article's lead image, with the
// licence and the credit that come with it.
//
// # Why this exists
//
// It is the fallback half of a location's cover image. The primary source is
// the og:image of a page the assistant has already read -- the venue's own
// photograph of itself -- which covers the hotels and restaurants Wikipedia
// has never heard of. This covers the other half: the landmarks with a good
// article and no useful official site.
//
// The model supplies an article *title*, never an image. That title is a
// lookup key, exactly as the address it proposes is a lookup key for
// internal/geocode, and for the same reason: what reaches the user comes from
// the upstream service rather than from the model. A hallucinated image URL
// would be a picture of the wrong building with no visible tell.
//
// # Why the licence travels with the image
//
// A Wikimedia image is freely licensed, not unencumbered: nearly all of them
// require attribution. Caravel is multi-user today and the backlog carries
// public share links, so an image stored with no record of where it came from
// is a problem waiting for the day somebody shares a trip. Storing the credit
// costs one column and answers the question permanently.
//
// # Shape
//
// Deliberately the same as internal/geocode: New returns nil when there is no
// endpoint, nil is the off switch, and a method on a nil client returns an
// error rather than panicking. No configuration is needed for the default,
// which is the point -- this works on a stock instance with no key and no
// search provider.
package wikimedia

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"strings"
	"time"

	"caravel/internal/buildinfo"
)

const (
	// Timeout bounds one lookup. Short: this runs at the very end of a run
	// that has already taken seconds, and a slow encyclopaedia should not hold
	// up a proposal that is otherwise ready.
	Timeout = 6 * time.Second
	// DefaultLanguage is the edition used when the caller names none. English
	// has the broadest coverage, so it is the right default -- but it is only
	// a default: see endpointFor.
	DefaultLanguage = "en"
	// maxBody caps what is read. Nothing legitimate here is large, and an
	// error page from a misconfigured proxy should not be read into memory in
	// full.
	maxBody = 1 << 20
	// minDimension rejects the icons, flags and chrome that share a page with
	// its photographs. A lead image below this is not a picture of the place.
	minDimension = 200
)

// Image is a lead image and everything needed to credit it.
type Image struct {
	// URL is the full-size image. Empty when the article has no lead image,
	// which is a normal answer rather than an error.
	URL string
	// ThumbURL is a smaller rendering, for a preview.
	ThumbURL string
	Width    int
	Height   int
	// Credit is the author line, as plain text. Wikimedia returns it as HTML;
	// it is flattened here, because it is displayed as text and markup from a
	// third party has no business reaching a page unescaped.
	Credit string
	// Licence is the short name, such as "CC BY-SA 4.0". Empty when the file
	// page does not state one, which happens.
	Licence string
	// DescriptionURL is the file's page on Wikimedia, where the full licence
	// and author details live. This is what a credit links to.
	DescriptionURL string
	// ArticleURL is the article the image was taken from.
	ArticleURL string
}

// Client looks up one Wikipedia API endpoint.
type Client struct {
	url  string
	http *http.Client
}

// New returns a client. An empty endpoint means "the Wikipedia edition for
// whatever language each lookup names", which is the normal case; a non-empty
// one pins every lookup to that URL, for a mirror or for a test.
//
// Unlike geocode.New this never returns nil for an empty argument: there is a
// working default and no reason to make an operator name it.
func New(endpoint string) *Client {
	return &Client{url: strings.TrimSpace(endpoint), http: &http.Client{Timeout: Timeout}}
}

// endpointFor picks the API endpoint for a language edition.
//
// The edition matters more than it looks. Article titles are not translations
// of each other -- the German article is "Brandenburger Tor" and the English
// one is "Brandenburg Gate" -- so a title proposed for one edition will simply
// miss in another, and there is no cross-edition fallback worth writing.
// Smaller places make this sharper still: Osnabrueck's Heger Tor has a German
// article and no English one, so an instance pinned to en would find nothing
// for a whole class of places its users care about.
//
// So the caller passes the language it asked the model to answer in, and the
// lookup goes to the matching edition.
func (c *Client) endpointFor(lang string) string {
	if c.url != "" {
		return c.url
	}
	return "https://" + normaliseLang(lang) + ".wikipedia.org/w/api.php"
}

// normaliseLang reduces a BCP-47 tag to a Wikipedia subdomain: "de-AT" becomes
// "de". Anything that is not plainly a language code falls back to the
// default, because this string ends up in a hostname.
func normaliseLang(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if i := strings.IndexAny(lang, "-_"); i > 0 {
		lang = lang[:i]
	}
	if len(lang) < 2 || len(lang) > 3 {
		return DefaultLanguage
	}
	for _, r := range lang {
		if r < 'a' || r > 'z' {
			return DefaultLanguage
		}
	}
	return lang
}

// ErrNotConfigured is returned by a nil Client, so a missed nil check fails
// legibly rather than panicking.
var ErrNotConfigured = errNotConfigured{}

type errNotConfigured struct{}

func (errNotConfigured) Error() string { return "wikimedia: no client is configured" }

// The wire shape. Separate from Image above so the JSON can follow the API
// rather than the other way round -- the same split geocode uses for
// Nominatim.
//
// One query does the whole job: pageimages gives the lead image and a
// thumbnail, imageinfo gives the licence and the credit, and redirects=1 means
// "Eiffel tower" finds the article at "Eiffel Tower".
type wireResponse struct {
	Query struct {
		Pages []struct {
			Title   string `json:"title"`
			Missing bool   `json:"missing"`
			// Original is the full-size lead image.
			Original struct {
				Source string `json:"source"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"original"`
			Thumbnail struct {
				Source string `json:"source"`
			} `json:"thumbnail"`
			FullURL string `json:"fullurl"`
		} `json:"pages"`
	} `json:"query"`
	Error *struct {
		Code string `json:"code"`
		Info string `json:"info"`
	} `json:"error"`
}

// fileInfo is the second call: the licence and author of one file.
type fileInfoResponse struct {
	Query struct {
		Pages []struct {
			ImageInfo []struct {
				DescriptionURL string `json:"descriptionurl"`
				ExtMetadata    struct {
					LicenseShortName struct {
						Value string `json:"value"`
					} `json:"LicenseShortName"`
					Artist struct {
						Value string `json:"value"`
					} `json:"Artist"`
				} `json:"extmetadata"`
			} `json:"imageinfo"`
		} `json:"pages"`
	} `json:"query"`
}

// LeadImage returns the lead image of the article with this title.
//
// A title that matches no article, or an article with no lead image, is not an
// error: it returns a zero Image and nil. The caller treats both as "nothing
// to offer", which is the same thing as far as a suggestion is concerned, and
// distinguishing them would only give the caller a branch with no different
// behaviour behind it.
func (c *Client) LeadImage(ctx context.Context, lang, title string) (Image, error) {
	if c == nil {
		return Image{}, ErrNotConfigured
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return Image{}, nil
	}

	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()

	endpoint := c.endpointFor(lang)

	var page wireResponse
	err := c.get(ctx, endpoint, map[string]string{
		"action":      "query",
		"prop":        "pageimages|info",
		"piprop":      "original|thumbnail",
		"pithumbsize": "640",
		"inprop":      "url",
		"redirects":   "1",
		"titles":      title,
	}, &page)
	if err != nil {
		return Image{}, err
	}
	if page.Error != nil {
		return Image{}, fmt.Errorf("wikimedia: %s: %s", page.Error.Code, page.Error.Info)
	}
	if len(page.Query.Pages) == 0 {
		return Image{}, nil
	}
	p := page.Query.Pages[0]
	if p.Missing || p.Original.Source == "" {
		return Image{}, nil
	}
	// Icons, flags and logos share a page with its photographs. A lead image
	// this small is not a picture of the place.
	if p.Original.Width < minDimension || p.Original.Height < minDimension {
		return Image{}, nil
	}

	img := Image{
		URL:        p.Original.Source,
		ThumbURL:   p.Thumbnail.Source,
		Width:      p.Original.Width,
		Height:     p.Original.Height,
		ArticleURL: p.FullURL,
	}

	// The licence is a second call, and a failure here is not fatal: an image
	// with no credit is still an image, and refusing to offer it because the
	// metadata call timed out would trade a working feature for a missing
	// attribution line. The caller can see Licence is empty and decide.
	var info fileInfoResponse
	if err := c.get(ctx, endpoint, map[string]string{
		"action": "query",
		"prop":   "imageinfo",
		"iiprop": "extmetadata|url",
		"titles": "File:" + fileNameOf(p.Original.Source),
	}, &info); err == nil && len(info.Query.Pages) > 0 && len(info.Query.Pages[0].ImageInfo) > 0 {
		ii := info.Query.Pages[0].ImageInfo[0]
		img.DescriptionURL = ii.DescriptionURL
		img.Licence = flattenHTML(ii.ExtMetadata.LicenseShortName.Value)
		img.Credit = flattenHTML(ii.ExtMetadata.Artist.Value)
	}

	return img, nil
}

func (c *Client) get(ctx context.Context, rawEndpoint string, params map[string]string, out any) error {
	endpoint, err := url.Parse(rawEndpoint)
	if err != nil {
		return err
	}
	q := endpoint.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	q.Set("format", "json")
	// The modern response shape: pages as an array rather than as a map keyed
	// by page id, which would otherwise need a map with one unpredictable key.
	q.Set("formatversion", "2")
	endpoint.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	// Wikimedia's policy asks callers to identify themselves, and anonymous
	// bulk traffic is what gets blocked.
	req.Header.Set("User-Agent", "Caravel/"+buildinfo.Version+" (self-hosted trip planner)")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("wikimedia: the API responded with status %d", resp.StatusCode)
	}
	return json.NewDecoder(&limitedReader{r: resp.Body, n: maxBody}).Decode(out)
}

// fileNameOf turns an upload URL into the file name the API wants. Wikimedia
// serves files from a path ending in the name, so the last segment is it.
func fileNameOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	parts := strings.Split(u.Path, "/")
	name := parts[len(parts)-1]
	// The path is already percent-encoded; the API wants it decoded.
	if decoded, err := url.PathUnescape(name); err == nil {
		return decoded
	}
	return name
}

// flattenHTML reduces the markup Wikimedia returns for an author or a licence
// to plain text.
//
// Artist in particular comes back as an anchor -- often several. This is shown
// as text and stored as text, so the markup has to go somewhere; dropping it
// here means no caller can accidentally render a third party's HTML.
// Deliberately crude: this is a credit line, not a document.
func flattenHTML(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch r {
		case '<':
			depth++
		case '>':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteRune(r)
			}
		}
	}
	out := strings.Join(strings.Fields(html.UnescapeString(b.String())), " ")
	if len([]rune(out)) > 300 {
		out = strings.TrimSpace(string([]rune(out)[:300])) + "…"
	}
	return out
}

// limitedReader caps a response body without pulling in io.LimitReader's
// silent truncation, which would produce a JSON decode error that reads as
// "malformed response" rather than "too large".
type limitedReader struct {
	r interface{ Read([]byte) (int, error) }
	n int
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, fmt.Errorf("wikimedia: the response exceeded %d bytes", maxBody)
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= n
	return n, err
}
