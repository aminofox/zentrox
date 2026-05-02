package middleware

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/aminofox/zentrox/v2"
)

type TimeoutConfig struct {
	Duration  time.Duration
	OnTimeout func(*zentrox.Context)
}

func Timeout(d time.Duration) zentrox.Handler {
	return TimeoutWithConfig(TimeoutConfig{Duration: d})
}

func TimeoutWithConfig(cfg TimeoutConfig) zentrox.Handler {
	if cfg.Duration <= 0 {
		return func(c *zentrox.Context) { c.Next() }
	}
	if cfg.OnTimeout == nil {
		cfg.OnTimeout = func(c *zentrox.Context) {
			c.Fail(http.StatusGatewayTimeout, zentrox.MsgRequestTimeout)
		}
	}

	return func(c *zentrox.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), cfg.Duration)
		defer cancel()

		c.Request = c.Request.WithContext(ctx)
		c.Next()

		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return
		}

		status := 0
		if rw, ok := c.Writer.(interface{ Status() int }); ok {
			status = rw.Status()
		}
		if status == 0 && !c.Aborted() {
			cfg.OnTimeout(c)
		}
	}
}
