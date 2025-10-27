package middleware

import (
	"log"
	"net/http"
	"strings"

	"github.com/aminofox/zentrox"
)

// ErrorHandlerConfig controls logging and default messages.
type ErrorHandlerConfig struct {
	// If true, logs panic values using log.Printf.
	LogPanic bool

	// Default message for 500 if none provided.
	DefaultMessage string
}

// DefaultErrorHandler returns a sensible default configuration.
func DefaultErrorHandler() ErrorHandlerConfig {
	return ErrorHandlerConfig{
		LogPanic:       true,
		DefaultMessage: "internal server error",
	}
}

// ErrorHandler standardizes error responses, converts panics to HTTP error payloads,
// and honors content negotiation for application/problem+json when requested by clients.
//
// Behavior:
//   - Panic: recovers, optionally logs (cfg.LogPanic), and writes 500 response
//     as problem+json if client accepts it, otherwise JSON {code,message}.
//   - c.Error() set by handlers: writes that error as-is (zentrox.HTTPError),
//     honoring problem+json when requested.
//   - For unknown errors: maps to 500 with cfg.DefaultMessage and includes detail
//     text in a safe envelope.
//
// Notes:
//   - This middleware does NOT swallow the chain prematurely: it runs c.Next(),
//     then checks c.Error() and c.Aborted() to decide what to write.
//   - Prefer installing this early in the middleware stack to cover most failures.
func ErrorHandler(cfg ErrorHandlerConfig) zentrox.Handler {
	if cfg.DefaultMessage == "" {
		cfg.DefaultMessage = "internal server error"
	}

	return func(c *zentrox.Context) {
		// Recover from panics and render a 500 error.
		defer func() {
			if r := recover(); r != nil {
				if cfg.LogPanic {
					log.Printf("panic: %v", r)
				}
				// Respect content negotiation for problem+json.
				wantsProblem := strings.Contains(strings.ToLower(c.GetHeader("Accept")), "application/problem+json")
				if wantsProblem {
					c.Problem(http.StatusInternalServerError, "about:blank", cfg.DefaultMessage, "", c.Request.URL.Path, nil)
				} else {
					c.JSON(http.StatusInternalServerError, zentrox.HTTPError{
						Code:    http.StatusInternalServerError,
						Message: cfg.DefaultMessage,
					})
				}
				c.Abort()
			}
		}()

		// Continue the chain.
		c.Next()
		if c.Aborted() {
			return
		}

		// If a handler recorded an error, render it now.
		if err := c.Error(); err != nil {
			wantsProblem := strings.Contains(strings.ToLower(c.GetHeader("Accept")), "application/problem+json")

			switch e := err.(type) {
			case zentrox.HTTPError:
				// Application-level error with explicit status code.
				if wantsProblem {
					// Map to RFC 9457 problem+json; use Message as title and include detail when present.
					var detail string
					if e.Detail != nil {
						// avoid leaking sensitive detail to end users; use as-is if you intend to expose
						if s, ok := e.Detail.(string); ok {
							detail = s
						}
					}
					c.Problem(e.Code, "about:blank", e.Message, detail, c.Request.URL.Path, nil)
				} else {
					c.JSON(e.Code, e)
				}
				c.Abort()
				return

			default:
				// Unknown error type → map to 500.
				if wantsProblem {
					c.Problem(http.StatusInternalServerError, "about:blank", cfg.DefaultMessage, "", c.Request.URL.Path, nil)
				} else {
					c.JSON(http.StatusInternalServerError, zentrox.HTTPError{
						Code:    http.StatusInternalServerError,
						Message: cfg.DefaultMessage,
						Detail:  err.Error(),
					})
				}
				c.Abort()
				return
			}
		}
	}
}
