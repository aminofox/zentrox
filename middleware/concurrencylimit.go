package middleware

import (
	"net/http"
	"runtime"
	"time"

	"github.com/aminofox/zentrox"
)

type ConcurrencyLimitConfig struct {
	MaxConcurrent int
	QueueTimeout  time.Duration
	OnLimit       func(*zentrox.Context)
}

func DefaultConcurrencyLimit() ConcurrencyLimitConfig {
	max := runtime.GOMAXPROCS(0) * 128
	if max < 128 {
		max = 128
	}
	return ConcurrencyLimitConfig{
		MaxConcurrent: max,
		QueueTimeout:  0,
		OnLimit: func(c *zentrox.Context) {
			c.Fail(http.StatusServiceUnavailable, zentrox.MsgServerBusy)
		},
	}
}

func ConcurrencyLimit(cfg ConcurrencyLimitConfig) zentrox.Handler {
	if cfg.MaxConcurrent <= 0 {
		return func(c *zentrox.Context) { c.Next() }
	}
	if cfg.OnLimit == nil {
		cfg.OnLimit = func(c *zentrox.Context) {
			c.Fail(http.StatusServiceUnavailable, zentrox.MsgServerBusy)
		}
	}

	sem := make(chan struct{}, cfg.MaxConcurrent)

	return func(c *zentrox.Context) {
		if cfg.QueueTimeout <= 0 {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				c.Next()
				return
			default:
				cfg.OnLimit(c)
				c.Abort()
				return
			}
		}

		timer := time.NewTimer(cfg.QueueTimeout)
		defer timer.Stop()

		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
			c.Next()
			return
		case <-timer.C:
			cfg.OnLimit(c)
			c.Abort()
			return
		case <-c.Done():
			c.Abort()
			return
		}
	}
}
