package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/aminofox/zentrox"
)

type RateLimitConfig struct {
	Rate       float64
	Burst      float64
	KeyFunc    func(*zentrox.Context) string
	OnLimit    func(*zentrox.Context)
	StaleAfter time.Duration
}

type bucket struct {
	tokens float64
	last   time.Time
	seen   time.Time
}

func DefaultRateLimit() RateLimitConfig {
	return RateLimitConfig{
		Rate:  20,
		Burst: 40,
		KeyFunc: func(c *zentrox.Context) string {
			return c.RealIP()
		},
		OnLimit: func(c *zentrox.Context) {
			c.JSON(http.StatusTooManyRequests, map[string]any{
				"code":    http.StatusTooManyRequests,
				"message": zentrox.MsgTooManyRequests,
			})
		},
		StaleAfter: 10 * time.Minute,
	}
}

func RateLimit(cfg RateLimitConfig) zentrox.Handler {
	if cfg.Rate <= 0 {
		cfg.Rate = 20
	}
	if cfg.Burst <= 0 {
		cfg.Burst = cfg.Rate * 2
	}
	if cfg.KeyFunc == nil {
		cfg.KeyFunc = func(c *zentrox.Context) string { return c.RealIP() }
	}
	if cfg.OnLimit == nil {
		cfg.OnLimit = func(c *zentrox.Context) {
			c.JSON(http.StatusTooManyRequests, map[string]any{"code": 429, "message": zentrox.MsgTooManyRequests})
		}
	}
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = 10 * time.Minute
	}

	var mu sync.Mutex
	buckets := make(map[string]*bucket)
	lastCleanup := time.Now()

	cleanup := func(now time.Time) {
		for k, b := range buckets {
			if now.Sub(b.seen) > cfg.StaleAfter {
				delete(buckets, k)
			}
		}
	}

	allow := func(key string, now time.Time) bool {
		if key == "" {
			key = "global"
		}
		b := buckets[key]
		if b == nil {
			b = &bucket{tokens: cfg.Burst, last: now, seen: now}
			buckets[key] = b
		}

		delta := now.Sub(b.last).Seconds()
		if delta > 0 {
			b.tokens += delta * cfg.Rate
			if b.tokens > cfg.Burst {
				b.tokens = cfg.Burst
			}
		}
		b.last = now
		b.seen = now

		if b.tokens < 1 {
			return false
		}
		b.tokens--
		return true
	}

	return func(c *zentrox.Context) {
		now := time.Now()
		key := cfg.KeyFunc(c)

		mu.Lock()
		if len(buckets) > 0 && now.Sub(lastCleanup) >= time.Minute {
			cleanup(now)
			lastCleanup = now
		}
		ok := allow(key, now)
		mu.Unlock()

		if !ok {
			cfg.OnLimit(c)
			c.Abort()
			return
		}
		c.Next()
	}
}
