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
	)
	if err != nil {
		panic(err)
	}

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

## What the client sends

- Path: onion `/search` or `/news` (`q`, `source=web`, `spellcheck=0`, optional `offset` / `tf`)
- Cookies: `useLocation=0`, `summarizer=0`, `safesearch=off` unless you change it
- No Ask Brave, no Google fallback mixing, no metrics opt-in, no accounts

## License

[MIT-0](LICENSE). Use it however you want. Credit is appreciated, not required.

The software is provided **as is**, without warranty of any kind, including fitness for a purpose or that it will keep working. There is no promise of updates, maintenance, or support. You are solely responsible for what you do with it. The authors are not liable for any claim, damage, or other liability arising from the software or its use.
