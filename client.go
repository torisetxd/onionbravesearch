package onionbravesearch

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/proxy"
)

const (
	DefaultOnionBase = "https://search.brave4u7jddbv7cyviptqjc7jusxh72uik7zt6adtckl5f4nwy2v72qd.onion/"
	DefaultProxy     = "127.0.0.1:9050"
	DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var (
	ErrBlocked     = errors.New("onionbravesearch: request blocked")
	ErrEmptyQuery  = errors.New("onionbravesearch: empty query")
	ErrBadPage     = errors.New("onionbravesearch: page must be between 1 and 10")
	ErrBadCategory = errors.New("onionbravesearch: unsupported category")
)

type Client struct {
	http      *http.Client
	baseURL   string
	userAgent string
}

type Option func(*clientConfig) error

type clientConfig struct {
	proxy      string
	baseURL    string
	userAgent  string
	timeout    time.Duration
	httpClient *http.Client
	insecure   bool
}

func New(opts ...Option) (*Client, error) {
	cfg := clientConfig{
		proxy:     DefaultProxy,
		baseURL:   DefaultOnionBase,
		userAgent: DefaultUserAgent,
		timeout:   90 * time.Second,
	}
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if !strings.HasSuffix(cfg.baseURL, "/") {
		cfg.baseURL += "/"
	}

	hc := cfg.httpClient
	if hc == nil {
		transport, err := socksTransport(cfg.proxy, cfg.insecure)
		if err != nil {
			return nil, err
		}
		hc = &http.Client{
			Timeout:   cfg.timeout,
			Transport: transport,
		}
	}

	return &Client{
		http:      hc,
		baseURL:   cfg.baseURL,
		userAgent: cfg.userAgent,
	}, nil
}

func WithProxy(addr string) Option {
	return func(c *clientConfig) error {
		c.proxy = addr
		return nil
	}
}

func WithBaseURL(raw string) Option {
	return func(c *clientConfig) error {
		if raw == "" {
			return errors.New("onionbravesearch: empty base url")
		}
		c.baseURL = raw
		return nil
	}
}

func WithUserAgent(ua string) Option {
	return func(c *clientConfig) error {
		if ua != "" {
			c.userAgent = ua
		}
		return nil
	}
}

func WithTimeout(d time.Duration) Option {
	return func(c *clientConfig) error {
		c.timeout = d
		return nil
	}
}

func WithHTTPClient(hc *http.Client) Option {
	return func(c *clientConfig) error {
		c.httpClient = hc
		return nil
	}
}

func WithInsecureSkipVerify(v bool) Option {
	return func(c *clientConfig) error {
		c.insecure = v
		return nil
	}
}

func (c *Client) Search(ctx context.Context, query string, opt SearchOptions) (*Response, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrEmptyQuery
	}
	if opt.Category == "" {
		opt.Category = CategoryWeb
	}
	if opt.Category != CategoryWeb && opt.Category != CategoryNews {
		return nil, ErrBadCategory
	}
	if opt.Page <= 0 {
		opt.Page = 1
	}
	if opt.Page > 10 {
		return nil, ErrBadPage
	}
	if opt.Country == "" {
		opt.Country = "us"
	}
	if opt.UILang == "" {
		opt.UILang = "en-us"
	}
	if opt.SafeSearch == "" {
		opt.SafeSearch = SafeSearchOff
	}

	raw, err := c.fetch(ctx, query, opt)
	if err != nil {
		return nil, err
	}
	res, err := ParseSearchHTML(raw)
	if err != nil {
		return nil, err
	}
	res.Query = query
	return res, nil
}

func (c *Client) fetch(ctx context.Context, query string, opt SearchOptions) ([]byte, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("base url: %w", err)
	}
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Cache-Control", "no-cache")

	cookies := []string{
		"safesearch=" + string(opt.SafeSearch),
		"useLocation=0",
		"summarizer=0",
		"country=" + strings.ToLower(opt.Country),
		"ui_lang=" + strings.ToLower(opt.UILang),
	}
	req.Header.Set("Cookie", strings.Join(cookies, "; "))

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("onionbravesearch: unexpected status %d", resp.StatusCode)
	}
	return body, nil
}

func socksTransport(proxyAddr string, insecure bool) (*http.Transport, error) {
	addr := strings.TrimSpace(proxyAddr)
	addr = strings.TrimPrefix(addr, "socks5h://")
	addr = strings.TrimPrefix(addr, "socks5://")
	addr = strings.TrimPrefix(addr, "socks://")
	if addr == "" {
		return nil, errors.New("onionbravesearch: empty proxy address")
	}

	dialer, err := proxy.SOCKS5("tcp", addr, nil, &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("socks5 dialer: %w", err)
	}

	contextDial := func(ctx context.Context, network, address string) (net.Conn, error) {
		if cd, ok := dialer.(proxy.ContextDialer); ok {
			return cd.DialContext(ctx, network, address)
		}
		return dialer.Dial(network, address)
	}

	return &http.Transport{
		DialContext:           contextDial,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: insecure,
		},
	}, nil
}
