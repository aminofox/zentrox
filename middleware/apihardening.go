package middleware

import (
	"time"

	"github.com/aminofox/zentrox"
)

type APIHardeningConfig struct {
	RequestID        RequestIDConfig
	SecurityHeaders  SecurityHeadersConfig
	HTTPProtection   HTTPProtectionConfig
	BodyLimit        BodyLimitConfig
	ConcurrencyLimit ConcurrencyLimitConfig
	RateLimit        RateLimitConfig
	Timeout          time.Duration
}

type APIHardeningFastConfig struct {
	SecurityHeaders  SecurityHeadersConfig
	HTTPProtection   HTTPProtectionConfig
	BodyLimit        BodyLimitConfig
	ConcurrencyLimit ConcurrencyLimitConfig
	RateLimit        RateLimitConfig
}

func DefaultAPIHardeningConfig() APIHardeningConfig {
	return APIHardeningConfig{
		RequestID:        DefaultRequestID(),
		SecurityHeaders:  DefaultSecurityHeaders(),
		HTTPProtection:   DefaultHTTPProtection(),
		BodyLimit:        DefaultBodyLimit(),
		ConcurrencyLimit: DefaultConcurrencyLimit(),
		RateLimit:        DefaultRateLimit(),
		Timeout:          2 * time.Second,
	}
}

func APIHardening(cfg APIHardeningConfig) []zentrox.Handler {
	out := make([]zentrox.Handler, 0, 7)
	out = append(out,
		RequestID(cfg.RequestID),
		SecurityHeaders(cfg.SecurityHeaders),
		HTTPProtection(cfg.HTTPProtection),
		BodyLimit(cfg.BodyLimit),
		ConcurrencyLimit(cfg.ConcurrencyLimit),
		RateLimit(cfg.RateLimit),
	)
	if cfg.Timeout > 0 {
		out = append(out, Timeout(cfg.Timeout))
	}
	return out
}

func DefaultAPIHardening() []zentrox.Handler {
	return APIHardening(DefaultAPIHardeningConfig())
}

func DefaultAPIHardeningFastConfig() APIHardeningFastConfig {
	return APIHardeningFastConfig{
		SecurityHeaders:  DefaultSecurityHeaders(),
		HTTPProtection:   DefaultHTTPProtection(),
		BodyLimit:        DefaultBodyLimit(),
		ConcurrencyLimit: DefaultConcurrencyLimit(),
		RateLimit:        DefaultRateLimit(),
	}
}

func APIHardeningFast(cfg APIHardeningFastConfig) []zentrox.Handler {
	out := make([]zentrox.Handler, 0, 5)
	out = append(out,
		SecurityHeaders(cfg.SecurityHeaders),
		HTTPProtection(cfg.HTTPProtection),
		BodyLimit(cfg.BodyLimit),
		ConcurrencyLimit(cfg.ConcurrencyLimit),
		RateLimit(cfg.RateLimit),
	)
	return out
}

func DefaultAPIHardeningFast() []zentrox.Handler {
	return APIHardeningFast(DefaultAPIHardeningFastConfig())
}
