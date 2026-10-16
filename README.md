# onionbravesearch

Unofficial Go clients for [Brave Search](https://search.brave.com/) and [DuckDuckGo](https://duckduckgo.com/) over their onion services, via a local Tor SOCKS5 proxy, plus a unified client that tries DuckDuckGo first and falls back to Brave.

The point is **non-traceable, free search** for agents and private work: no search API keys, no clearnet search hop, no account. Traffic stays inside Tor and terminates at the search engine’s hidden service (no exit node), and every request carries a coherent, randomized browser fingerprint instead of Go’s very recognizable `crypto/tls` one.

This is not Brave’s official API. It scrapes the public onion frontend.

## Privacy — Brave Search (notice re-read 2026-10-16)

Quoted from the [Brave Search privacy notice](https://search.brave.com/help/privacy-policy). **Brave rewrote this notice after the 2026-09-01 draft of this README**, so the old headline quote — “we don’t collect personal information about you, your device or your searches” — is gone. What it says now:

> To ensure that Brave Search is relevant, useful, and secure, we collect and store de-identified search query data which includes the plain text of the search query itself, the device platform, operating system, country, language and the date. We do not store any additional data associated with the search query that could link it with an individual user, their device, or IP address. Search queries are not grouped based on the user, device or IP address the search query originated from.

So: query text and device platform/OS are **retained**, just de-identified and not grouped by user or IP.

The only IP handling they document for the search service itself:

> We temporarily process IP addresses to detect and prevent bots in order to ensure the integrity and availability of the service for all users. IP addresses are not retained but are deleted within seconds.

On ads (this client does not click ads):

> We also infer the country location associated with clicks and views of Ads on Brave Search. This is based on a device IP address. We do not retain IP addresses.

On “near me” results (this client sends `useLocation=0`):

> With anonymous local results, when you search for “cafes near me” or “food near me” Brave Search will use the IP address broadcast by your device but without sharing that IP address and without storing it.

Ask Brave is **not used** here. For completeness they state conversation state can live up to 24h on AWS (now via Nebius Token Factory, with zero data retention), and: “Brave does not retain your IP address.”

One more clause was added in the rewrite, and it is the one that matters for bot defence:

> Brave Search uses local state in your browser for service integrity purposes. This state does not reveal specific user behavior and cannot be used to profile or track you. You can delete this data through your browser settings, though doing so may degrade the security or usability of Brave Search.

### What that means on this path

Queries are **de-identified but retained**, not zero-data-retention. The plain-text query, device platform/OS, country, language and date are stored; they cannot be tied back to you, your device or an IP. An earlier version of this README called that “effectively zero-data-retention” — that conclusion no longer holds.

This client only talks to:

`https://search.brave4u7jddbv7cyviptqjc7jusxh72uik7zt6adtckl5f4nwy2v72qd.onion/`

over Tor. There is no clearnet exit. Whatever the integrity path processes is a **Tor relay / rendezvous point**, not your IP.

That is an interpretation of the public notice as of **2026-10-16**, plus how onion services work. It is not a guarantee from Brave, and it is not legal advice.

### DuckDuckGo

Not covered by the notice above. DuckDuckGo’s own privacy policy is the source of truth for it; the same onion-service reasoning applies (Brave’s or DuckDuckGo’s integrity checks see a Tor rendezvous point, not you). This repo makes no claim about it beyond “the traffic path is onion-only”.

## Requirements

A running Tor SOCKS5 proxy, default `127.0.0.1:9050`.

### Docker

From this repo:

```bash
docker compose up -d --wait
```

That builds a local Tor image, binds SOCKS5 to `127.0.0.1:9050` only, and waits until the daemon is listening. First bootstrap is usually 10–30s. Stop with `docker compose down`.

If something else already owns `9050`, change the host side in `compose.yaml` and pass the same address to `WithProxy`.

### systemd

On Arch / CachyOS the distro `tor` package already ships a unit that listens on `127.0.0.1:9050`:

```bash
sudo pacman -S tor
sudo systemctl enable --now tor
```

```text
go get github.com/torisetxd/onionbravesearch
```

## Usage

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/torisetxd/onionbravesearch"
)

func main() {
	c, err := onionbravesearch.New(
		onionbravesearch.WithProxy("127.0.0.1:9050"),
		onionbravesearch.WithTimeout(90*time.Second),
		onionbravesearch.WithProfile(onionbravesearch.ProfileRandom),
		onionbravesearch.WithRetries(3),
	)
	if err != nil {
		panic(err)
	}
	defer c.Close()

	res, err := c.Search(context.Background(), "golang socks5", onionbravesearch.SearchOptions{
		Category: onionbravesearch.CategoryWeb,
		Page:     1,
	})
	if err != nil {
		panic(err)
	}

	for _, r := range res.Results {
		fmt.Println(r.Title, r.URL)
	}
}
```

`SearchOptions` also covers news (`CategoryNews`), safesearch, country, UI language, time range, and spellcheck.

Typical latency after Tor is bootstrapped: about **3–8 seconds** per query.

### DuckDuckGo

```go
c, err := onionbravesearch.NewDuckDuckGo(
	onionbravesearch.WithProxy("127.0.0.1:9050"),
	onionbravesearch.WithTimeout(90*time.Second),
)
if err != nil {
	panic(err)
}
defer c.Close()

res, err := c.Search(context.Background(), "golang socks5", onionbravesearch.SearchOptions{})
```

That talks to `https://duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad.onion/html` (the no-JS interface), with `kl` / `kp` / `df` / `s` / `dc` mapped from the same `SearchOptions`. DuckDuckGo has no news vertical here, so `CategoryNews` returns `ErrBadCategory`.

### Unified: DuckDuckGo first, Brave second

```go
u, err := onionbravesearch.NewUnified(
	onionbravesearch.WithProxy("127.0.0.1:9050"),
	onionbravesearch.WithRetries(2),
)
if err != nil {
	panic(err)
}
defer u.Close()

res, err := u.Search(context.Background(), "golang socks5", onionbravesearch.SearchOptions{})
fmt.Println(res.Engine, len(res.Results)) // "duckduckgo" or "brave"
```

`Unified.Search` walks the engines in order and returns the first usable answer. An engine is skipped when it cannot serve the requested category (so `CategoryNews` goes straight to Brave) and — unless `WithFallbackOnEmpty(false)` — when it answers with zero results. `Response.Engine` says which backend answered. The engines run on separate clients, so they get separate circuits and separate fingerprints.

### Engines at a glance

| Engine | Constructor | Onion host | Endpoint |
| --- | --- | --- | --- |
| Brave Search | `New` / `NewBrave` | `search.brave4u7jddbv7cyviptqjc7jusxh72uik7zt6adtckl5f4nwy2v72qd.onion` | `/search`, `/news` |
| DuckDuckGo | `NewDuckDuckGo` | `duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad.onion` | `/html` |

Point them somewhere else with `WithBraveBaseURL` / `WithDuckDuckGoBaseURL` (or plain `WithBaseURL` for a single-engine client) — handy for the clearnet hosts while debugging without Tor.

## Fingerprints

Two layers get randomized:

- **TLS** — the ClientHello is a real browser parroted through [uTLS](https://github.com/refraction-networking/utls) (Chrome 131/133, Firefox 120, Safari 16, iOS Safari 14), not Go’s `crypto/tls` fingerprint. ALPN is rewritten to `http/1.1` so the handshake stays honest with the HTTP/1.1 the transport actually speaks.
- **HTTP** — `User-Agent`, `sec-ch-ua*`, `Sec-Fetch-*`, `Accept`, `Accept-Encoding` and the header *order* all come from the same profile, so the metadata is coherent instead of a mismatched pile of defaults.

A profile is drawn at random per client, and re-rolled on every circuit rotation. Within one circuit it stays fixed — a real browser does not change its UA between two searches, so a per-request mix would be the giveaway rather than the fix.

The transport writes HTTP/1.1 by hand over the tunneled TLS connection (`transport.go`), which is what makes header order and coherence controllable at all; `net/http` always sorts headers alphabetically.

`Accept-Encoding` advertises `gzip, deflate, br, zstd` and responses are decoded transparently.

Deliberately **not** randomized: the functional cookies (`safesearch`, `useLocation`, `summarizer`, `country`, `ui_lang`) because they drive the results. Their order is shuffled, and any `Set-Cookie` the edge returns (integrity/challenge state) is kept in memory and echoed back for the life of the client.

Pin a family with `WithProfile(onionbravesearch.ProfileChrome)` (`ProfileFirefox`, `ProfileSafari`, `ProfileIOSSafari`); `ProfileRandom` is the default.

## Rate limits (429)

`429` and `503` surface as a `*RateLimitError` (it unwraps to `ErrRateLimited`) and are retried automatically. Between attempts the client:

1. switches to a **new Tor circuit** — each client dials SOCKS5 with a unique username, and Tor’s default `IsolateSOCKSAuth` routes each username over its own circuit. No control port, no authentication, and not subject to the ~10s `NEWNYM` throttle.
2. **re-rolls the browser profile**, so the new circuit also gets a new fingerprint.
3. waits `Retry-After` / `X-RateLimit-Reset` when the server sends one, otherwise exponential backoff with jitter.

```go
onionbravesearch.WithRetries(3),                  // extra attempts after the first (default 2)
onionbravesearch.WithRetryBackoff(2*time.Second), // base backoff (default 2s)
onionbravesearch.WithCircuitRotation(false),      // opt out of rotating circuits
```

Over `.onion` there is no exit node and no client IP for the search engine to throttle on, so a 429 that survives rotation is not circuit-scoped — it is either request-fingerprint-scoped or the engine’s introduction-point DoS defence, and slowing down is the only honest lever left.

## What the client sends

- Path: onion `/search` or `/news` (`q`, `source=web`, `spellcheck=0`, optional `offset` / `tf`)
- Cookies: `useLocation=0`, `summarizer=0`, `safesearch=off` unless you change it, plus any integrity cookie the service sets
- DuckDuckGo: onion `/html` (`q`, `kl`, `kp`, optional `df`, `s`, `dc`), no cookies of its own
- Engine: HTTP/1.1 over a uTLS browser parroted through SOCKS5, with profile-consistent headers
- No Ask Brave, no Google fallback mixing, no metrics opt-in, no accounts

## License

[MIT-0](LICENSE). Use it however you want. Credit is appreciated, not required.

The software is provided **as is**, without warranty of any kind, including fitness for a purpose or that it will keep working. There is no promise of updates, maintenance, or support. You are solely responsible for what you do with it. The authors are not liable for any claim, damage, or other liability arising from the software or its use.
