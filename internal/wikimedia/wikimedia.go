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
	"sort"
	"strconv"
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
	// thumbSize is the width the API is asked to render previews at. A grid
	// cell, not a cover: the full-size original is what gets stored when
	// somebody picks one.
	thumbSize = "320"
	// searchArticles is how many matching articles contribute their lead
	// image. Five is a spread without being a page of near-duplicates.
	searchArticles = 5
	// searchFilesPerArticle caps the second call. Generous, because most of
	// what it lists is filtered out and asking for fewer would mean the filter
	// decides the count rather than the relevance does.
	searchFilesPerArticle = 50
	// minDimension rejects the icons, flags and chrome that share a page with
	// its photographs. A lead image below this is not a picture of the place.
	minDimension = 200
)

// Image is a lead image and everything needed to credit it.
type Image struct {
	// Title is what to call this picture in a list: the article title for a
	// lead image, the file name for one taken off an article. Empty for a
	// LeadImage lookup, which has nothing to disambiguate.
	Title string
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
	endpoint = strings.TrimSpace(endpoint)
	// The sentinel starts an in-process fixture encyclopaedia and pins this
	// client to it. See stub.go for why the browser suite needs one.
	if endpoint == StubURL {
		endpoint = startStubFixture()
	}
	return &Client{url: endpoint, http: &http.Client{Timeout: Timeout}}
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
			// Title identifies which file this is, which matters as soon as
			// more than one is asked for in a batch.
			Title     string `json:"title"`
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

// searchResponse is the generator=search call: matching articles, each with
// its lead image. Index carries the search rank.
type searchResponse struct {
	Query struct {
		Pages []struct {
			Index    int    `json:"index"`
			Title    string `json:"title"`
			Missing  bool   `json:"missing"`
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

// fileListResponse is the generator=images call: every file on one article,
// with enough about each to tell a photograph from a UI icon.
type fileListResponse struct {
	Query struct {
		Pages []struct {
			Title     string `json:"title"`
			ImageInfo []struct {
				URL            string `json:"url"`
				ThumbURL       string `json:"thumburl"`
				DescriptionURL string `json:"descriptionurl"`
				Mime           string `json:"mime"`
				Width          int    `json:"width"`
				Height         int    `json:"height"`
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

// Search is the free-text entry point, for the "Search for an image" control
// rather than for the assistant.
//
// The difference from LeadImage is who is choosing. LeadImage answers a
// *title* the model proposed, so one image is the whole answer -- there is
// nobody to pick between two. Here a person is looking at a grid, so the job
// is to offer a spread and let them judge, which is exactly the thing a blind
// agent cannot do.
//
// Two calls, in this order, because they fail differently:
//
//   - The lead image of each article the query matches. High precision and
//     one request: a search for "Meiji Shrine" answers with photographs of
//     Meiji Shrine, its inner garden, its outer garden and Yasukuni Shrine,
//     each already chosen by editors as the picture that represents its
//     article.
//   - Then the photographs *on* the best-matching article, which is where the
//     depth is -- roughly thirty for a well-covered landmark, against five
//     from the search.
//
// The second call is the one the plan warned might be too noisy to use, since
// generator=images lists every file on the page including icons, flags, maps
// and the Commons chrome. Measured on the English Meiji Shrine article: 41
// files, of which the raster-and-minimum-dimension filter keeps 32 and drops
// exactly the nine that are chrome -- three SVG symbols, a location map, two
// Shinto icons, the Commons logo, a UI icon and a video. So the gate is worth
// having and the call is worth making.
func (c *Client) Search(ctx context.Context, lang, query string, limit int) ([]Image, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	query = strings.TrimSpace(query)
	if query == "" || limit <= 0 {
		return nil, nil
	}

	// Twice Timeout, because this is up to three calls rather than two and it
	// runs while somebody watches a spinner rather than at the tail of a run
	// that has already taken seconds.
	ctx, cancel := context.WithTimeout(ctx, 2*Timeout)
	defer cancel()

	endpoint := c.endpointFor(lang)

	var found searchResponse
	err := c.get(ctx, endpoint, map[string]string{
		"action":       "query",
		"generator":    "search",
		"gsrsearch":    query,
		"gsrlimit":     strconv.Itoa(searchArticles),
		"gsrnamespace": "0",
		"prop":         "pageimages|info",
		"piprop":       "original|thumbnail",
		"pithumbsize":  thumbSize,
		"inprop":       "url",
	}, &found)
	if err != nil {
		return nil, err
	}
	if found.Error != nil {
		return nil, fmt.Errorf("wikimedia: %s: %s", found.Error.Code, found.Error.Info)
	}

	// The API returns the pages in whatever order it likes and puts the rank
	// in `index`, so sorting by it is what makes "the best match" mean
	// anything -- both for the order of the grid and for which article the
	// second call reads.
	pages := found.Query.Pages
	sort.SliceStable(pages, func(i, j int) bool { return pages[i].Index < pages[j].Index })

	var out []Image
	seen := map[string]bool{}
	add := func(img Image) {
		if img.URL == "" || seen[img.URL] || len(out) >= limit {
			return
		}
		seen[img.URL] = true
		out = append(out, img)
	}

	var leads []string
	for _, p := range pages {
		if p.Missing || p.Original.Source == "" {
			continue
		}
		if p.Original.Width < minDimension || p.Original.Height < minDimension {
			continue
		}
		add(Image{
			Title:      p.Title,
			URL:        p.Original.Source,
			ThumbURL:   p.Thumbnail.Source,
			Width:      p.Original.Width,
			Height:     p.Original.Height,
			ArticleURL: p.FullURL,
		})
		leads = append(leads, "File:"+fileNameOf(p.Original.Source))
	}

	// The photographs on the best match, if there is room left for them.
	if len(pages) > 0 && len(out) < limit {
		top := pages[0]
		var files fileListResponse
		if err := c.get(ctx, endpoint, map[string]string{
			"action":     "query",
			"titles":     top.Title,
			"generator":  "images",
			"gimlimit":   strconv.Itoa(searchFilesPerArticle),
			"prop":       "imageinfo",
			"iiprop":     "url|size|mime|extmetadata",
			"iiurlwidth": thumbSize,
		}, &files); err == nil {
			for _, p := range files.Query.Pages {
				if len(p.ImageInfo) == 0 {
					continue
				}
				ii := p.ImageInfo[0]
				if !isPhotograph(ii.Mime, ii.Width, ii.Height) {
					continue
				}
				add(Image{
					Title:          cleanFileTitle(p.Title),
					URL:            ii.URL,
					ThumbURL:       ii.ThumbURL,
					Width:          ii.Width,
					Height:         ii.Height,
					ArticleURL:     top.FullURL,
					DescriptionURL: ii.DescriptionURL,
					Licence:        flattenHTML(ii.ExtMetadata.LicenseShortName.Value),
					Credit:         flattenHTML(ii.ExtMetadata.Artist.Value),
				})
			}
		}
	}

	// The lead images arrived without their licences: pageimages says what the
	// picture is, not who took it. One batched imageinfo call fills them in.
	// Non-fatal for the same reason as in LeadImage -- an image with no credit
	// is still an image, and this is the call most likely to be the slow one.
	if len(leads) > 0 {
		var info fileInfoResponse
		if err := c.get(ctx, endpoint, map[string]string{
			"action": "query",
			"prop":   "imageinfo",
			"iiprop": "extmetadata|url",
			"titles": strings.Join(leads, "|"),
		}, &info); err == nil {
			byFile := map[string]struct{ credit, licence, desc string }{}
			for _, p := range info.Query.Pages {
				if len(p.ImageInfo) == 0 {
					continue
				}
				ii := p.ImageInfo[0]
				byFile[fileKey(p.Title)] = struct{ credit, licence, desc string }{
					credit:  flattenHTML(ii.ExtMetadata.Artist.Value),
					licence: flattenHTML(ii.ExtMetadata.LicenseShortName.Value),
					desc:    ii.DescriptionURL,
				}
			}
			for i := range out {
				if out[i].Licence != "" || out[i].Credit != "" {
					continue
				}
				if m, ok := byFile[fileKey(fileNameOf(out[i].URL))]; ok {
					out[i].Credit, out[i].Licence, out[i].DescriptionURL = m.credit, m.licence, m.desc
				}
			}
		}
	}

	return out, nil
}

// isPhotograph is the gate that keeps an article's icons, logos, maps and
// videos out of a grid of candidate cover photos. Raster only -- the chrome on
// a Wikipedia page is overwhelmingly SVG -- and above the same minimum
// dimension a lead image has to clear.
func isPhotograph(mime string, width, height int) bool {
	switch mime {
	case "image/jpeg", "image/png", "image/webp":
	default:
		return false
	}
	return width >= minDimension && height >= minDimension
}

// fileKey normalises a file name so a page title and an upload URL agree.
//
// They do not agree by default: the API says "File:Meiji Jingu 2023-3.jpg"
// and the URL says "Meiji_Jingu_2023-3.jpg", and the namespace prefix is
// itself translated ("Datei:" on the German edition), which is why the prefix
// is dropped by position rather than by name.
func fileKey(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ":"); i > 0 && i < 12 {
		s = s[i+1:]
	}
	return strings.ReplaceAll(strings.TrimSpace(s), "_", " ")
}

// cleanFileTitle turns "File:Meiji Jingu 2023-3.jpg" into "Meiji Jingu
// 2023-3", which is the closest thing to a caption these files have.
func cleanFileTitle(title string) string {
	title = fileKey(title)
	if i := strings.LastIndex(title, "."); i > 0 {
		title = title[:i]
	}
	return strings.TrimSpace(title)
}
