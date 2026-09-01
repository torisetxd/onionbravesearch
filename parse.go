package onionbravesearch

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

func ParseSearchHTML(raw []byte) (*Response, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parse html: %w", err)
	}

	page := strings.ToLower(doc.Find("title").First().Text())
	body := strings.ToLower(doc.Text())
	if strings.Contains(page, "just a moment") ||
		strings.Contains(body, "unusual traffic") ||
		strings.Contains(body, "verify you are human") ||
		doc.Find("#challenge-form, .cf-challenge, [data-cf-challenge]").Length() > 0 {
		return nil, ErrBlocked
	}

	out := &Response{
		Results: make([]Result, 0, 16),
		Related: make([]string, 0, 8),
	}

	doc.Find("div.snippet[data-type]").Each(func(_ int, s *goquery.Selection) {
		res, ok := parseSnippet(s)
		if !ok {
			return
		}
		out.Results = append(out.Results, res)
	})

	seen := map[string]struct{}{}
	doc.Find("a.related-query").Each(func(_ int, s *goquery.Selection) {
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

func parseSnippet(s *goquery.Selection) (Result, bool) {
	href := ""
	s.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		h, ok := a.Attr("href")
		if !ok {
			return true
		}
		h = strings.TrimSpace(h)
		if !isResultURL(h) {
			return true
		}
		href = h
		return false
	})
	if href == "" {
		return Result{}, false
	}

	titleSel := s.Find("div.title").First()
	title := strings.TrimSpace(titleSel.AttrOr("title", ""))
	if title == "" {
		title = compact(titleSel.Text())
	}
	if title == "" {
		return Result{}, false
	}

	desc := compact(s.Find("div.description").First().Text())
	if desc == "" {
		s.Find("div.generic-snippet div.content, div.content").EachWithBreak(func(_ int, c *goquery.Selection) bool {
			class, _ := c.Attr("class")
			if !hasClass(class, "content") || hasClass(class, "site-name-content") || hasClass(class, "result-content") {
				return true
			}
			desc = compact(c.Text())
			return desc == ""
		})
	}

	age := compact(s.Find(".age-snippet").First().Text())
	if age == "" {
		age = compact(s.Find("div.generic-snippet span.t-secondary").First().Text())
	}
	age = strings.Trim(age, "- ")
	if age != "" && strings.HasPrefix(desc, age) {
		desc = compact(strings.TrimLeft(strings.TrimPrefix(desc, age), "- "))
	}

	site := compact(s.Find(".site-name-content").First().Contents().First().Text())
	if site == "" {
		site = compact(s.Find(".site-name-content span, .site-name-content div").First().Text())
	}

	res := Result{
		Title:       title,
		URL:         href,
		Description: desc,
		Site:        site,
		DisplayURL:  compact(s.Find("cite.snippet-url").First().Text()),
		Age:         age,
		Favicon:     attr(s.Find("img.favicon").First(), "src"),
		Thumbnail:   firstThumb(s),
		Type:        strings.TrimSpace(s.AttrOr("data-type", "web")),
	}
	return res, true
}

func firstThumb(s *goquery.Selection) string {
	if v := attr(s.Find("a.thumbnail img").First(), "src"); v != "" {
		return v
	}
	return attr(s.Find(".snippet-thumbnail-wrapper img").First(), "src")
}

func isResultURL(h string) bool {
	u, err := url.Parse(h)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if strings.Contains(host, "search.brave.com") || strings.HasPrefix(host, "search.brave4u7jddbv") {
		return false
	}
	return true
}

func hasClass(class, token string) bool {
	for _, p := range strings.Fields(class) {
		if p == token {
			return true
		}
	}
	return false
}

func compact(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func attr(s *goquery.Selection, name string) string {
	v, _ := s.Attr(name)
	return strings.TrimSpace(v)
}
