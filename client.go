package onionbravesearch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultProxy     = "127.0.0.1:9050"
	DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"
)

var (
	ErrBlocked     = errors.New("onionbravesearch: request blocked")
	ErrEmptyQuery  = errors.New("onionbravesearch: empty query")
	ErrBadPage     = errors.New("onionbravesearch: page must be between 1 and 10")
	ErrBadCategory = errors.New("onionbravesearch: unsupported category")
	ErrRateLimited = errors.New("onionbravesearch: rate limited")
	ErrNoResults   = errors.New("onionbravesearch: no results")
)

// RateLimitError reports a throttled response. It unwraps to ErrRateLimited.
type RateLimitError struct {
	Status     int
	RetryAfter time.Duration
	Attempts   int
}

func (e *RateLimitError) Error() string {
	msg := fmt.Sprintf("onionbravesearch: unexpected status %d (rate limited)", e.Status)
	if e.Attempts > 1 {
		msg += fmt.Sprintf(", gave up after %d attempts", e.Attempts)
	}
	if e.RetryAfter > 0 {
		msg += fmt.Sprintf(", server asked to retry after %s", e.RetryAfter)
	}
	return msg
}

func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// engine describes one search backend: where to send the request, what to put
// on it, and how to read the answer back.
type engine interface {
	ID() string
	DefaultBase() string
	Supports(Category) bool
	Request(base *url.URL, query string, opt SearchOptions) *url.URL
	Cookies(opt SearchOptions) []string
	Parse(raw []byte) (*Response, error)
}

type Client struct {
	http    *http.Client
	tr      *transport
	ps      *profileState
	engine  engine
	baseURL string
	timeout time.Duration
	retries int
	backoff time.Duration
	rotate  bool

	mu      sync.Mutex
	cookies map[string]string
}

type Option func(*clientConfig) error

type clientConfig struct {
	proxy           string
	baseURL         string
	baseURLs        map[string]string
	userAgent       string
	profile         Profile
	timeout         time.Duration
	retries         int
	backoff         time.Duration
	rotate          bool
	race            int
	fallbackOnEmpty *bool
	httpClient      *http.Client
	insecure        bool
}

func defaultConfig() clientConfig {
	return clientConfig{
		proxy:   DefaultProxy,
		profile: ProfileRandom,
		timeout: 90 * time.Second,
		retries: 2,
		backoff: 2 * time.Second,
		rotate:  true,
		race:    defaultHandshakeRace,
	}
}

// baseFor resolves the base URL for one engine: engine-specific override, then
// generic override, then the engine default.
func (c clientConfig) baseFor(eng engine) string {
	base := eng.DefaultBase()
	if c.baseURL != "" {
		base = c.baseURL
	}
	if v, ok := c.baseURLs[eng.ID()]; ok {
		base = v
	}
	return base
}

func applyOptions(opts []Option) (clientConfig, error) {
	cfg := defaultConfig()
	for _, opt := range opts {
		if err := opt(&cfg); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

// New returns a client for Brave Search, i.e. NewBrave.
func New(opts ...Option) (*Client, error) { return NewBrave(opts...) }

// NewBrave returns a client for Brave Search's onion service.
func NewBrave(opts ...Option) (*Client, error) { return newClient(braveEngine{}, opts...) }

// NewDuckDuckGo returns a client for DuckDuckGo's onion service.
func NewDuckDuckGo(opts ...Option) (*Client, error) { return newClient(ddgEngine{}, opts...) }

func newClient(eng engine, opts ...Option) (*Client, error) {
	cfg, err := applyOptions(opts)
	if err != nil {
		return nil, err
	}
	baseURL := cfg.baseFor(eng)
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	pool, ok := profilePool(cfg.profile)
	if !ok {
		return nil, fmt.Errorf("onionbravesearch: unknown profile %q", cfg.profile)
	}
	ps := newProfileState(pool, cfg.userAgent)

	c := &Client{
		engine:  eng,
		baseURL: baseURL,
		ps:      ps,
		timeout: cfg.timeout,
		retries: cfg.retries,
		backoff: cfg.backoff,
		rotate:  cfg.rotate,
		cookies: map[string]string{},
	}

	if cfg.httpClient != nil {
		c.http = cfg.httpClient
	} else {
		c.tr = newTransport(cfg.proxy, cfg.insecure, ps, cfg.timeout, cfg.race)
		c.http = &http.Client{Timeout: cfg.timeout, Transport: c.tr}
	}

	return c, nil
}

// Engine reports which backend this client talks to.
func (c *Client) Engine() string { return c.engine.ID() }

// Close drops pooled connections.
func (c *Client) Close() {
	if c.tr != nil {
		c.tr.closeIdle()
	}
}

func WithProxy(addr string) Option {
	return func(c *clientConfig) error {
		c.proxy = addr
		return nil
	}
}

// WithBaseURL overrides the base for whichever engine is being built. Use
// WithBraveBaseURL / WithDuckDuckGoBaseURL to target one engine specifically,
// which is what the unified client needs.
func WithBaseURL(raw string) Option {
	return func(c *clientConfig) error {
		if raw == "" {
			return errors.New("onionbravesearch: empty base url")
		}
		c.baseURL = raw
		return nil
	}
}

func WithBraveBaseURL(raw string) Option { return withEngineBase("brave", raw) }

func WithDuckDuckGoBaseURL(raw string) Option { return withEngineBase("duckduckgo", raw) }

func withEngineBase(id, raw string) Option {
	return func(c *clientConfig) error {
		if raw == "" {
			return errors.New("onionbravesearch: empty base url")
		}
		if c.baseURLs == nil {
			c.baseURLs = map[string]string{}
		}
		c.baseURLs[id] = raw
		return nil
	}
}

func WithUserAgent(ua string) Option {
	return func(c *clientConfig) error {
		c.userAgent = ua
		return nil
	}
}

// WithProfile pins the browser fingerprint family. ProfileRandom (the default)
// draws a fresh identity per circuit rotation.
func WithProfile(p Profile) Option {
	return func(c *clientConfig) error {
		c.profile = p
		return nil
	}
}

func WithTimeout(d time.Duration) Option {
	return func(c *clientConfig) error {
		c.timeout = d
		return nil
	}
}

// WithRetries sets how many extra attempts are made after a throttle or a
// dropped circuit. Rotation happens between attempts.
func WithRetries(n int) Option {
	return func(c *clientConfig) error {
		if n < 0 {
			n = 0
		}
		c.retries = n
		return nil
	}
}

func WithRetryBackoff(d time.Duration) Option {
	return func(c *clientConfig) error {
		c.backoff = d
		return nil
	}
}

// WithCircuitRotation toggles switching to a new Tor circuit (and fingerprint)
// on retry.
func WithCircuitRotation(v bool) Option {
	return func(c *clientConfig) error {
		c.rotate = v
		return nil
	}
}

// WithHandshakeRace sets how many parallel Tor circuits race to open a new
// connection; the first to finish wins and the rest are pooled. Circuit
// construction latency is set by the slowest relay on the path, so racing
// a few independent circuits shortens the tail. Defaults to
// defaultHandshakeRace; 1 or less pays for a single circuit.
func WithHandshakeRace(n int) Option {
	return func(c *clientConfig) error {
		if n < 1 {
			n = 1
		}
		c.race = n
		return nil
	}
}

// WithFallbackOnEmpty controls whether the unified client moves on to the next
// engine when an engine answers successfully but with zero results. Defaults
// to true.
func WithFallbackOnEmpty(v bool) Option {
	return func(c *clientConfig) error {
		c.fallbackOnEmpty = &v
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

func normalizeOptions(opt SearchOptions, eng engine) (SearchOptions, error) {
	if opt.Category == "" {
		opt.Category = CategoryWeb
	}
	if opt.Category != CategoryWeb && opt.Category != CategoryNews {
		return opt, ErrBadCategory
	}
	if !eng.Supports(opt.Category) {
		return opt, ErrBadCategory
	}
	if opt.Page <= 0 {
		opt.Page = 1
	}
	if opt.Page > 10 {
		return opt, ErrBadPage
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
	return opt, nil
}

func (c *Client) Search(ctx context.Context, query string, opt SearchOptions) (*Response, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrEmptyQuery
	}
	opt, err := normalizeOptions(opt, c.engine)
	if err != nil {
		return nil, err
	}

	var (
		attempts int
	)
	for attempt := 0; ; attempt++ {
		attempts++
		raw, err := c.fetch(ctx, query, opt)
		var res *Response
		if err == nil {
			res, err = c.engine.Parse(raw)
		}
		if err == nil {
			res.Query = query
			res.Engine = c.engine.ID()
			return res, nil
		}
		if attempt >= c.retries || !retryable(err) {
			var rle *RateLimitError
			if errors.As(err, &rle) {
				rle.Attempts = attempts
			}
			return nil, err
		}
		if c.rotate {
			c.rotateIdentity()
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.retryDelay(attempt+1, err)):
		}
	}
}

func (c *Client) rotateIdentity() {
	if c.tr != nil {
		c.tr.rotate()
		return
	}
	c.ps.rotate()
}

func (c *Client) retryDelay(attempt int, err error) time.Duration {
	var rle *RateLimitError
	if errors.As(err, &rle) && rle.RetryAfter > 0 {
		return rle.RetryAfter
	}
	if c.backoff <= 0 {
		return 0
	}
	d := c.backoff << (attempt - 1)
	if d > 30*time.Second {
		d = 30 * time.Second
	}
	return d + time.Duration(rand.Int64N(int64(d/2+1)))
}

func retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrRateLimited) || errors.Is(err, ErrBlocked) {
		return true
	}
	var ne interface{ Timeout() bool }
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "unexpected status 5") ||
		strings.Contains(msg, "read response") ||
		strings.Contains(msg, "write request") ||
		strings.Contains(msg, "socks5")
}

func (c *Client) fetch(ctx context.Context, query string, opt SearchOptions) ([]byte, error) {
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("base url: %w", err)
	}
	u := c.engine.Request(base, query, opt)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	prof := c.ps.get()
	for k, v := range prof.values {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept-Language", acceptLanguage(opt.UILang))
	req.Header.Set("Cookie", c.cookieHeader(c.engine.Cookies(opt)))

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()

	c.storeCookies(resp)

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusServiceUnavailable {
			return nil, &RateLimitError{
				Status:     resp.StatusCode,
				RetryAfter: retryAfter(resp),
			}
		}
		return nil, fmt.Errorf("onionbravesearch: unexpected status %d", resp.StatusCode)
	}
	return body, nil
}

// acceptLanguage derives a plausible quality-weighted list from the UI language
// so it stays consistent with the ui_lang cookie.
func acceptLanguage(uiLang string) string {
	base := strings.TrimSpace(strings.ToLower(uiLang))
	if base == "" {
		base = "en-us"
	}
	parts := strings.Split(base, "-")
	primary := parts[0]
	region := ""
	if len(parts) > 1 {
		region = primary + "-" + strings.ToUpper(parts[1])
	}
	w := []string{"0.9", "0.8", "0.7", "0.95"}
	if region != "" && region != primary {
		return fmt.Sprintf("%s,%s;q=%s", region, primary, pick(w))
	}
	return fmt.Sprintf("%s;q=%s", base, pick(w))
}

func pick(all []string) string {
	return all[rand.IntN(len(all))]
}

func (c *Client) cookieHeader(extra []string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	managed := map[string]bool{
		"safesearch": true, "uselocation": true, "summarizer": true,
		"country": true, "ui_lang": true,
	}
	pairs := make([]string, 0, len(c.cookies)+len(extra))
	pairs = append(pairs, extra...)

	names := make([]string, 0, len(c.cookies))
	for name := range c.cookies {
		if managed[strings.ToLower(name)] {
			continue
		}
		names = append(names, name)
	}
	rand.Shuffle(len(names), func(i, j int) { names[i], names[j] = names[j], names[i] })
	for _, name := range names {
		pairs = append(pairs, name+"="+c.cookies[name])
	}
	rand.Shuffle(len(pairs), func(i, j int) { pairs[i], pairs[j] = pairs[j], pairs[i] })
	return strings.Join(pairs, "; ")
}

// storeCookies keeps any integrity cookie the edge hands back so subsequent
// requests look like the same session.
func (c *Client) storeCookies(resp *http.Response) {
	set := resp.Cookies()
	if len(set) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ck := range set {
		if ck.Name == "" || ck.MaxAge < 0 {
			delete(c.cookies, ck.Name)
			continue
		}
		c.cookies[ck.Name] = ck.Value
	}
}

func retryAfter(resp *http.Response) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
		if t, err := http.ParseTime(v); err == nil {
			if d := time.Until(t); d > 0 {
				return d
			}
		}
	}
	for _, h := range []string{"X-RateLimit-Reset", "X-Retry-After"} {
		if v := resp.Header.Get(h); v != "" {
			if secs, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && secs >= 0 {
				return time.Duration(secs) * time.Second
			}
		}
	}
	return 0
}
