package middleware

import (
	"crypto/rand"
	"encoding/hex"

	"github.com/aminofox/zentrox"
)

type RequestIDConfig struct {
	HeaderName string
	ContextKey string
	Generator  func() string
}

func DefaultRequestID() RequestIDConfig {
	return RequestIDConfig{
		HeaderName: zentrox.XRequestID,
		ContextKey: zentrox.RequestID,
		Generator:  generateRequestID,
	}
}

func RequestID(cfg RequestIDConfig) zentrox.Handler {
	if cfg.HeaderName == "" {
		cfg.HeaderName = zentrox.XRequestID
	}
	if cfg.ContextKey == "" {
		cfg.ContextKey = zentrox.RequestID
	}
	if cfg.Generator == nil {
		cfg.Generator = generateRequestID
	}

	return func(c *zentrox.Context) {
		rid := c.GetHeader(cfg.HeaderName)
		if rid == "" {
			rid = cfg.Generator()
		}
		c.Set(cfg.ContextKey, rid)
		c.SetHeader(cfg.HeaderName, rid)
		c.Next()
	}
}

func generateRequestID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
