# onionbravesearch

Unofficial Go client for [Brave Search](https://search.brave.com/) over its [onion service](https://search.brave4u7jddbv7cyviptqjc7jusxh72uik7zt6adtckl5f4nwy2v72qd.onion/), via a local Tor SOCKS5 proxy.

The point is **ZDR-shaped, non-traceable, free search** for agents and private work: no Brave Search API key, no clearnet search hop, no account. Traffic stays inside Tor and terminates at Brave’s hidden service (no exit node).

This is not Brave’s official API. It scrapes the public onion frontend.

## Privacy (as of 2026-09-01)

Quoted from [Brave Search privacy notice](https://search.brave.com/help/privacy-policy) as it stood when this was written. Terms can change; re-read the source.

Brave’s default claim:

> Brave Search is designed to be private by default. We don’t collect personal information about you, your device or your searches. We also don’t transmit information to the web that could be used to profile you or track you or learn anything about you. Your searches are private to YOU.

The only IP handling they document for the search service itself:

> We temporarily process IP addresses to detect and prevent bots in order to ensure the integrity and availability of the service for all users. IP addresses are not retained but are deleted within seconds.

On ads (this client does not click ads):

> We also infer the country location associated with clicks and views of Ads on Brave Search. This is based on a device IP address. We do not retain IP addresses.

On “near me” results (this client sends `useLocation=0`):

> With anonymous local results, when you search for “cafes near me” or “food near me” Brave Search will use the IP address broadcast by your device but without sharing that IP address and without storing it.

Ask Brave is **not used** here. For completeness they state conversation state can live up to 24h on AWS, and: “Brave does not retain your IP address.”

### What that means on this path

By those terms, Brave Search is **effectively zero-data-retention** for queries: they say they do not keep personal data, device data, or search history, and any IP taken for bot defense is dropped within seconds.

This client only talks to:

`https://search.brave4u7jddbv7cyviptqjc7jusxh72uik7zt6adtckl5f4nwy2v72qd.onion/`

over Tor. There is no clearnet exit. The IP Brave can see is a **Tor relay**, not yours. So the residual “IP logging” in their integrity path, if it happens at all on the onion service, is logging **Tor**, not you.

That is an interpretation of the public notice as of **2026-09-01**, plus how onion services work. It is not a guarantee from Brave, and it is not legal advice.

## Requirements

A running Tor SOCKS5 proxy, default `127.0.0.1:9050`.

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
