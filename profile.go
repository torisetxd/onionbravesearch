package onionbravesearch

import (
	"math/rand/v2"
	"sync"

	utls "github.com/refraction-networking/utls"
)

// Profile selects a browser fingerprint family (TLS ClientHello + HTTP metadata).
type Profile string

const (
	// ProfileRandom picks a random profile per client, and a fresh one again on
	// every circuit rotation.
	ProfileRandom Profile = "random"
	// ProfileChrome pins the fingerprint to a Chromium desktop build.
	ProfileChrome Profile = "chrome"
	// ProfileFirefox pins the fingerprint to a Firefox desktop build.
	ProfileFirefox Profile = "firefox"
	// ProfileSafari pins the fingerprint to a Safari macOS build.
	ProfileSafari Profile = "safari"
	// ProfileIOSSafari pins the fingerprint to an iOS Safari build.
	ProfileIOSSafari Profile = "ios-safari"
)

// tlsHello is the field we care about; keeps profile literals readable.
type profile struct {
	name   string
	hello  utls.ClientHelloID
	order  []string
	values map[string]string
}

func (p *profile) clone() *profile {
	c := *p
	c.order = append([]string(nil), p.order...)
	c.values = make(map[string]string, len(p.values))
	for k, v := range p.values {
		c.values[k] = v
	}
	return &c
}

var (
	chromeOrder = []string{
		"Host", "Connection", "Sec-Ch-Ua", "Sec-Ch-Ua-Mobile", "Sec-Ch-Ua-Platform",
		"Upgrade-Insecure-Requests", "User-Agent", "Accept",
		"Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-User", "Sec-Fetch-Dest",
		"Accept-Encoding", "Accept-Language", "Cookie",
	}
	firefoxOrder = []string{
		"Host", "User-Agent", "Accept", "Accept-Language", "Accept-Encoding", "Dnt",
		"Upgrade-Insecure-Requests", "Sec-Fetch-Dest", "Sec-Fetch-Mode", "Sec-Fetch-Site",
		"Sec-Fetch-User", "Cookie",
	}
	safariOrder = []string{
		"Host", "Accept", "Sec-Fetch-Site", "Sec-Fetch-Dest", "Sec-Fetch-Mode", "Sec-Fetch-User",
		"Accept-Language", "Accept-Encoding", "User-Agent", "Connection",
		"Upgrade-Insecure-Requests", "Cookie",
	}

	chromeAccept  = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7"
	firefoxAccept = "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/png,image/svg+xml,*/*;q=0.8"
	safariAccept  = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
)

func chromeProfile(ua, version, platform string) *profile {
	brands := []string{
		`"Not(A:Brand";v="99", "Google Chrome";v="` + version + `", "Chromium";v="` + version + `"`,
		`"Chromium";v="` + version + `", "Not(A:Brand";v="24", "Google Chrome";v="` + version + `"`,
		`"Google Chrome";v="` + version + `", "Chromium";v="` + version + `", "Not(A:Brand";v="99"`,
	}
	return &profile{
		name:  "chrome",
		hello: utls.HelloChrome_Auto,
		order: chromeOrder,
		values: map[string]string{
			"Connection":                "keep-alive",
			"User-Agent":                ua,
			"Sec-Ch-Ua":                 brands[rand.IntN(len(brands))],
			"Sec-Ch-Ua-Mobile":          "?0",
			"Sec-Ch-Ua-Platform":        `"` + platform + `"`,
			"Upgrade-Insecure-Requests": "1",
			"Accept":                    chromeAccept,
			"Sec-Fetch-Site":            "none",
			"Sec-Fetch-Mode":            "navigate",
			"Sec-Fetch-User":            "?1",
			"Sec-Fetch-Dest":            "document",
			"Accept-Encoding":           "gzip, deflate, br, zstd",
		},
	}
}

func firefoxProfile(ua string) *profile {
	return &profile{
		name:  "firefox",
		hello: utls.HelloFirefox_Auto,
		order: firefoxOrder,
		values: map[string]string{
			"User-Agent":                ua,
			"Accept":                    firefoxAccept,
			"Accept-Encoding":           "gzip, deflate, br, zstd",
			"Dnt":                       "1",
			"Upgrade-Insecure-Requests": "1",
			"Sec-Fetch-Dest":            "document",
			"Sec-Fetch-Mode":            "navigate",
			"Sec-Fetch-Site":            "none",
			"Sec-Fetch-User":            "?1",
		},
	}
}

func safariProfile(ua string, ios bool) *profile {
	name := "safari"
	if ios {
		name = "ios-safari"
	}
	return &profile{
		name:  name,
		hello: utls.HelloSafari_Auto,
		order: safariOrder,
		values: map[string]string{
			"User-Agent":                ua,
			"Accept":                    safariAccept,
			"Accept-Encoding":           "gzip, deflate, br",
			"Upgrade-Insecure-Requests": "1",
			"Sec-Fetch-Site":            "none",
			"Sec-Fetch-Dest":            "document",
			"Sec-Fetch-Mode":            "navigate",
			"Sec-Fetch-User":            "?1",
			"Connection":                "keep-alive",
		},
	}
}

func chromePool() []*profile {
	return []*profile{
		chromeProfile("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36", "133", "Windows"),
		chromeProfile("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36", "131", "Windows"),
		chromeProfile("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36", "133", "macOS"),
		chromeProfile("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36", "133", "Linux"),
	}
}

func firefoxPool() []*profile {
	return []*profile{
		firefoxProfile("Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0"),
		firefoxProfile("Mozilla/5.0 (X11; Linux x86_64; rv:120.0) Gecko/20100101 Firefox/120.0"),
		firefoxProfile("Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:120.0) Gecko/20100101 Firefox/120.0"),
	}
}

func safariPool() []*profile {
	return []*profile{
		safariProfile("Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 Safari/605.1.15", false),
	}
}

func iosSafariPool() []*profile {
	return []*profile{
		safariProfile("Mozilla/5.0 (iPhone; CPU iPhone OS 14_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.0 Mobile/15E148 Safari/604.1", true),
	}
}

func profilePool(p Profile) ([]*profile, bool) {
	var pool []*profile
	switch p {
	case "", ProfileRandom:
		pool = append(pool, chromePool()...)
		pool = append(pool, firefoxPool()...)
		pool = append(pool, safariPool()...)
		pool = append(pool, iosSafariPool()...)
	case ProfileChrome:
		pool = chromePool()
	case ProfileFirefox:
		pool = firefoxPool()
	case ProfileSafari:
		pool = safariPool()
	case ProfileIOSSafari:
		pool = iosSafariPool()
	default:
		return nil, false
	}
	return pool, true
}

// profileState holds the sticky-per-circuit fingerprint and swaps it on rotation.
type profileState struct {
	mu      sync.Mutex
	pool    []*profile
	current *profile
	ua      string
}

func newProfileState(pool []*profile, uaOverride string) *profileState {
	s := &profileState{pool: pool, ua: uaOverride}
	s.current = s.pick()
	return s
}

func (s *profileState) pick() *profile {
	p := s.pool[rand.IntN(len(s.pool))].clone()
	if s.ua != "" {
		p.values["User-Agent"] = s.ua
	}
	return p
}

func (s *profileState) get() *profile {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

func (s *profileState) rotate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = s.pick()
}
