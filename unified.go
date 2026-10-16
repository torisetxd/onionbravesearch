package onionbravesearch

import (
	"context"
	"errors"
	"fmt"
)

// Unified tries several search backends in order, DuckDuckGo first and Brave
// second by default, and returns the first usable answer.
type Unified struct {
	clients         []*Client
	fallbackOnEmpty bool
}

// NewUnified builds a DuckDuckGo-first, Brave-second client. Options apply to
// both backends; use WithDuckDuckGoBaseURL / WithBraveBaseURL to point them at
// different hosts.
func NewUnified(opts ...Option) (*Unified, error) {
	cfg, err := applyOptions(opts)
	if err != nil {
		return nil, err
	}
	if cfg.baseURL != "" {
		return nil, errors.New("onionbravesearch: use WithDuckDuckGoBaseURL/WithBraveBaseURL with the unified client")
	}

	ddg, err := NewDuckDuckGo(opts...)
	if err != nil {
		return nil, err
	}
	brave, err := NewBrave(opts...)
	if err != nil {
		ddg.Close()
		return nil, err
	}

	onEmpty := true
	if cfg.fallbackOnEmpty != nil {
		onEmpty = *cfg.fallbackOnEmpty
	}

	return &Unified{
		clients:         []*Client{ddg, brave},
		fallbackOnEmpty: onEmpty,
	}, nil
}

// NewUnifiedFrom wraps ready-made clients and tries them in the given order.
func NewUnifiedFrom(clients ...*Client) (*Unified, error) {
	if len(clients) == 0 {
		return nil, errors.New("onionbravesearch: no clients given")
	}
	return &Unified{clients: clients, fallbackOnEmpty: true}, nil
}

// Engines lists the backends in fallback order.
func (u *Unified) Engines() []string {
	ids := make([]string, 0, len(u.clients))
	for _, c := range u.clients {
		ids = append(ids, c.Engine())
	}
	return ids
}

// Clients exposes the underlying clients, in fallback order.
func (u *Unified) Clients() []*Client { return u.clients }

func (u *Unified) Close() {
	for _, c := range u.clients {
		c.Close()
	}
}

// Search walks the engines in order. An engine is skipped when it cannot serve
// the requested category, and - when fallbackOnEmpty is on - when it answers
// with no results at all.
func (u *Unified) Search(ctx context.Context, query string, opt SearchOptions) (*Response, error) {
	var (
		errs  []error
		empty *Response
		tried bool
	)

	for _, c := range u.clients {
		opt, err := normalizeOptions(opt, c.engine)
		if err != nil {
			continue
		}
		tried = true

		res, err := c.Search(ctx, query, opt)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.Engine(), err))
			if ctx.Err() != nil {
				break
			}
			continue
		}
		if len(res.Results) > 0 || !u.fallbackOnEmpty {
			return res, nil
		}
		if empty == nil {
			empty = res
		}
		errs = append(errs, fmt.Errorf("%s: %w", c.Engine(), ErrNoResults))
	}

	if !tried {
		return nil, ErrBadCategory
	}
	if empty != nil {
		return empty, nil
	}
	if len(errs) == 0 {
		return nil, ErrNoResults
	}
	return nil, errors.Join(errs...)
}
