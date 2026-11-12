package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/csx-3323730970/gocr/internal/obs"
)

// Cache is a content-addressed response cache.
type Cache interface {
	Get(ctx context.Context, key string) (payload []byte, ok bool, err error)
	Put(ctx context.Context, key string, payload []byte, tokens int, ttl time.Duration) error
}

// CachingProvider stores complete responses keyed by namespace + request.
// Cache failures never fail a review.
type CachingProvider struct {
	Inner     Provider
	Cache     Cache
	Namespace string
	TTL       time.Duration
}

func NewCachingProvider(inner Provider, cache Cache, namespace string) *CachingProvider {
	return &CachingProvider{Inner: inner, Cache: cache, Namespace: namespace, TTL: 24 * time.Hour}
}

func (p *CachingProvider) Name() string { return p.Inner.Name() }

func (p *CachingProvider) Complete(ctx context.Context, req Request) (Response, error) {
	key, keyErr := cacheKey(p.Namespace, req)
	if keyErr == nil && p.Cache != nil {
		if raw, ok, err := p.Cache.Get(ctx, key); err == nil && ok {
			var cached Response
			if json.Unmarshal(raw, &cached) == nil {
				obs.CacheHitsTotal.Inc()
				return cached, nil
			}
		}
	}

	resp, err := p.Inner.Complete(ctx, req)
	if err != nil {
		return resp, err
	}
	if keyErr == nil && p.Cache != nil {
		if raw, marshalErr := json.Marshal(resp); marshalErr == nil {
			ttl := p.TTL
			if ttl <= 0 {
				ttl = 24 * time.Hour
			}
			_ = p.Cache.Put(ctx, key, raw, resp.Usage.Total(), ttl)
		}
	}
	return resp, nil
}

func cacheKey(namespace string, req Request) (string, error) {
	raw, err := json.Marshal(struct {
		Namespace string  `json:"namespace"`
		Request   Request `json:"request"`
	}{namespace, req})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
