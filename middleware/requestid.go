package middleware

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/aminofox/zentrox"
)

// RequestIDConfig controls how request IDs are created/propagated.
type RequestIDConfig struct {
	HeaderName      string        // default: "X-Request-ID"
	AllowFromHeader bool          // accept incoming header if present
	Generator       func() string // if nil, use 16-byte random hex
	ContextKey      string        // default: "request_id"
}

func DefaultRequestID() RequestIDConfig {
	return RequestIDConfig{
		HeaderName:      zentrox.XRequestID,
		AllowFromHeader: true,
		ContextKey:      zentrox.RequestID,
	}
}

func RequestID(cfg RequestIDConfig) zentrox.Handler {
	if cfg.HeaderName == "" {
		cfg.HeaderName = zentrox.XRequestID
	}
	if cfg.ContextKey == "" {
		cfg.ContextKey = zentrox.RequestID
	}
	gen := cfg.Generator
	if gen == nil {
		gen = func() string {
			var b [16]byte
			_, _ = rand.Read(b[:])
			return hex.EncodeToString(b[:])
		}
	}

	return func(c *zentrox.Context) {
		id := ""
		if cfg.AllowFromHeader {
			id = c.GetHeader(cfg.HeaderName)
		}
		if id == "" {
			id = gen()
		}
		c.Set(cfg.ContextKey, id)
		c.SetHeader(cfg.HeaderName, id)
		c.Forward()
	}
}
