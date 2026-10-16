package onionbravesearch

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// DefaultDuckDuckGoBase is DuckDuckGo's official v3 onion service.
const DefaultDuckDuckGoBase = "https://duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad.onion/"

// ddgEngine drives DuckDuckGo's HTML-only (no-JS) interface, which is what its
// onion service exposes at /html.
type ddgEngine struct{}

func (ddgEngine) ID() string { return "duckduckgo" }

func (ddgEngine) DefaultBase() string { return DefaultDuckDuckGoBase }

// The HTML endpoint has no news vertical; that request is left to another engine.
func (ddgEngine) Supports(c Category) bool { return c == CategoryWeb }

func (ddgEngine) Request(base *url.URL, query string, opt SearchOptions) *url.URL {
	u := *base
	u.Path = strings.TrimRight(u.Path, "/") + "/html"

	q := url.Values{}
	q.Set("q", query)
	q.Set("kl", ddgRegion(opt.Country))
	q.Set("kp", ddgSafeSearch(opt.SafeSearch))
	if df := ddgTimeRange(opt.TimeRange); df != "" {
		q.Set("df", df)
	}
	if opt.Page > 1 {
		offset := (opt.Page - 1) * ddgPageSize
		q.Set("s", fmt.Sprintf("%d", offset))
		q.Set("dc", fmt.Sprintf("%d", ddgPageSize))
	}
	u.RawQuery = q.Encode()
	return &u
}

func (ddgEngine) Cookies(SearchOptions) []string { return nil }

func (ddgEngine) Parse(raw []byte) (*Response, error) { return ParseDuckDuckGoHTML(raw) }

const ddgPageSize = 30

func ddgSafeSearch(s SafeSearch) string {
	switch s {
	case SafeSearchStrict:
		return "1"
	case SafeSearchModerate:
		return "-1"
	default:
		return "-2"
	}
}

func ddgTimeRange(t TimeRange) string {
	switch t {
	case TimeDay:
		return "d"
	case TimeWeek:
		return "w"
	case TimeMonth:
		return "m"
	case TimeYear:
		return "y"
	default:
		return ""
	}
}

// ddgRegion maps a bare country code onto DDG's kl value, which wants the
// language appended ("us-en", "wt-wt" for worldwide).
func ddgRegion(country string) string {
	c := strings.ToLower(strings.TrimSpace(country))
	if c == "" {
		return "wt-wt"
	}
	if c == "wt" || c == "wt-wt" {
		return "wt-wt"
	}
	if strings.Contains(c, "-") {
		return c
	}
	return c + "-" + "en"
}

// ParseDuckDuckGoHTML parses the HTML-only result page.
func ParseDuckDuckGoHTML(raw []byte) (*Response, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	title := strings.ToLower(doc.Find("title").First().Text())
	body := strings.ToLower(doc.Text())
	if strings.Contains(title, "anomaly") ||
		strings.Contains(body, "bots use duckduckgo too") ||
		strings.Contains(body, "unusual traffic") ||
		strings.Contains(body, "verify you are human") ||
		strings.Contains(body, "unfortunately, bots") {
		return nil, ErrBlocked
	}

	out := &Response{
		Results: make([]Result, 0, 16),
		Related: make([]string, 0, 8),
	}

	doc.Find("div.result, div.web-result").Each(func(_ int, s *goquery.Selection) {
		if isDDGAd(s) {
			return
		}
		res, ok := parseDDGSnippet(s)
		if !ok {
			return
		}
		out.Results = append(out.Results, res)
	})

	seen := map[string]struct{}{}
	doc.Find("div.related-searches a, .related-searches__link, a.result__a.related-searches__link").Each(func(_ int, s *goquery.Selection) {
		q := compact(s.Text())
		if q == "" {
			return
		}
		if _, exists := seen[q]; exists {
			return
		}
		seen[q] = struct{}{}
		out.Related = append(out.Related, q)
	})

	return out, nil
}

func isDDGAd(s *goquery.Selection) bool {
	cls, _ := s.Attr("class")
	if hasClass(cls, "result--ad") || hasClass(cls, "result--ad--small") ||
		hasClass(cls, "result--sponsored") || hasClass(cls, "js-result-sponsored") {
		return true
	}
	return s.Find(".badge--ad, .result__badge--ad, .result__sponsored").Length() > 0
}

func parseDDGSnippet(s *goquery.Selection) (Result, bool) {
	link := s.Find("a.result__a").First()
	if link.Length() == 0 {
		link = s.Find("h2 a[href]").First()
	}
	if link.Length() == 0 {
		return Result{}, false
	}

	title := compact(link.Text())
	href := ddgResultURL(attr(link, "href"))
	if title == "" || href == "" {
		return Result{}, false
	}

	desc := compact(s.Find(".result__snippet").First().Text())
	if desc == "" {
		desc = compact(s.Find("a.result__snippet").First().Text())
	}

	display := compact(s.Find(".result__url").First().Text())
	if display == "" {
		display = compact(s.Find(".result__extras__url").First().Text())
	}

	site := display
	if u, err := url.Parse(href); err == nil && u.Hostname() != "" {
		site = u.Hostname()
	}

	return Result{
		Title:       title,
		URL:         href,
		Description: desc,
		Site:        site,
		DisplayURL:  display,
		Type:        "web",
	}, true
}

// ddgResultURL unwraps the /l/?uddg= redirector DDG wraps every result in.
func ddgResultURL(href string) string {
	href = strings.TrimSpace(href)
	if href == "" {
		return ""
	}
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if u.Path == "/l/" || u.Path == "/l" {
		if target := u.Query().Get("uddg"); target != "" {
			return target
		}
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	if u.Host == "" {
		return ""
	}
	return href
}
