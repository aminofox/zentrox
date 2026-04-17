package middleware

import "github.com/aminofox/zentrox"

// SecurityHeadersConfig controls baseline response hardening headers.
// Set a field to "-" to explicitly disable that header.
type SecurityHeadersConfig struct {
	XContentTypeOptions string
	XFrameOptions       string
	ReferrerPolicy      string
	Extra               map[string]string
}

type headerPair struct {
	k string
	v string
}

func DefaultSecurityHeaders() SecurityHeadersConfig {
	return SecurityHeadersConfig{
		XContentTypeOptions: "nosniff",
		XFrameOptions:       "DENY",
		ReferrerPolicy:      "strict-origin-when-cross-origin",
	}
}

func SecurityHeaders(cfg SecurityHeadersConfig) zentrox.Handler {
	cfg.XContentTypeOptions = withDefaultHeader(cfg.XContentTypeOptions, "nosniff")
	cfg.XFrameOptions = withDefaultHeader(cfg.XFrameOptions, "DENY")
	cfg.ReferrerPolicy = withDefaultHeader(cfg.ReferrerPolicy, "strict-origin-when-cross-origin")

	extras := make([]headerPair, 0, len(cfg.Extra))
	for k, v := range cfg.Extra {
		if k == "" || v == "" {
			continue
		}
		extras = append(extras, headerPair{k: k, v: v})
	}

	return func(c *zentrox.Context) {
		h := c.Writer.Header()

		if cfg.XContentTypeOptions != "" && h.Get(zentrox.HeaderXContentTypeOptions) == "" {
			h.Set(zentrox.HeaderXContentTypeOptions, cfg.XContentTypeOptions)
		}
		if cfg.XFrameOptions != "" && h.Get(zentrox.HeaderXFrameOptions) == "" {
			h.Set(zentrox.HeaderXFrameOptions, cfg.XFrameOptions)
		}
		if cfg.ReferrerPolicy != "" && h.Get(zentrox.HeaderReferrerPolicy) == "" {
			h.Set(zentrox.HeaderReferrerPolicy, cfg.ReferrerPolicy)
		}

		for _, kv := range extras {
			if h.Get(kv.k) == "" {
				h.Set(kv.k, kv.v)
			}
		}

		c.Next()
	}
}

func withDefaultHeader(value, fallback string) string {
	if value == "-" {
		return ""
	}
	if value == "" {
		return fallback
	}
	return value
}
