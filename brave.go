package onionbravesearch

import (
	"fmt"
	"net/url"
	"strings"
)

// DefaultOnionBase is Brave Search's official onion service.
const DefaultOnionBase = "https://search.brave4u7jddbv7cyviptqjc7jusxh72uik7zt6adtckl5f4nwy2v72qd.onion/"

// braveEngine drives Brave Search's public onion frontend.
type braveEngine struct{}

func (braveEngine) ID() string { return "brave" }

func (braveEngine) DefaultBase() string { return DefaultOnionBase }

func (braveEngine) Supports(c Category) bool { return c == CategoryWeb || c == CategoryNews }

func (braveEngine) Request(base *url.URL, query string, opt SearchOptions) *url.URL {
	u := *base
	u.Path = strings.TrimRight(u.Path, "/") + "/" + string(opt.Category)

	q := url.Values{}
	q.Set("q", query)
	q.Set("source", "web")
	if opt.Spellcheck {
		q.Set("spellcheck", "1")
	} else {
		q.Set("spellcheck", "0")
	}
	if opt.Page > 1 {
		q.Set("offset", fmt.Sprintf("%d", opt.Page-1))
	}
	if opt.TimeRange != TimeAny {
		q.Set("tf", string(opt.TimeRange))
	}
	u.RawQuery = q.Encode()
	return &u
}

func (braveEngine) Cookies(opt SearchOptions) []string {
	return []string{
		"safesearch=" + string(opt.SafeSearch),
		"useLocation=0",
		"summarizer=0",
		"country=" + strings.ToLower(opt.Country),
		"ui_lang=" + strings.ToLower(opt.UILang),
	}
}

func (braveEngine) Parse(raw []byte) (*Response, error) { return ParseSearchHTML(raw) }
