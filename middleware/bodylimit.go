package middleware

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/aminofox/zentrox/v2"
)

type BodyLimitConfig struct {
	MaxBytes int64
	OnLimit  func(*zentrox.Context)
}

func DefaultBodyLimit() BodyLimitConfig {
	return BodyLimitConfig{
		MaxBytes: 1 << 20, // 1 MiB
		OnLimit: func(c *zentrox.Context) {
			c.Fail(http.StatusRequestEntityTooLarge, zentrox.MsgPayloadTooLarge)
		},
	}
}

func BodyLimit(cfg BodyLimitConfig) zentrox.Handler {
	if cfg.MaxBytes <= 0 {
		return func(c *zentrox.Context) { c.Next() }
	}
	if cfg.OnLimit == nil {
		cfg.OnLimit = func(c *zentrox.Context) {
			c.Fail(http.StatusRequestEntityTooLarge, zentrox.MsgPayloadTooLarge)
		}
	}

	return func(c *zentrox.Context) {
		if c.Request.ContentLength > cfg.MaxBytes {
			cfg.OnLimit(c)
			c.Abort()
			return
		}
		if !requestMayHaveBody(c.Request) {
			c.Next()
			return
		}

		tracker := &maxBytesTracker{
			ReadCloser: http.MaxBytesReader(c.Writer, c.Request.Body, cfg.MaxBytes),
		}
		c.Request.Body = tracker

		c.Next()

		if !tracker.exceeded {
			return
		}

		status := 0
		if rw, ok := c.Writer.(interface{ Status() int }); ok {
			status = rw.Status()
		}
		if status == 0 && !c.Aborted() {
			cfg.OnLimit(c)
		}
	}
}

func requestMayHaveBody(r *http.Request) bool {
	if r == nil || r.Body == nil || r.Body == http.NoBody {
		return false
	}
	if r.ContentLength > 0 {
		return true
	}
	if len(r.TransferEncoding) > 0 {
		return true
	}
	if strings.TrimSpace(r.Header.Get(zentrox.HeaderContentLength)) != "" {
		return true
	}
	return r.ContentLength < 0
}

type maxBytesTracker struct {
	io.ReadCloser
	exceeded bool
}

func (m *maxBytesTracker) Read(p []byte) (int, error) {
	n, err := m.ReadCloser.Read(p)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			m.exceeded = true
		}
	}
	return n, err
}
