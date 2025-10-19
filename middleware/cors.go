package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/aminofox/zentrox"
)

type CORSConfig struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           int // seconds
}

func DefaultCORS() CORSConfig {
	return CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"*"},
		ExposeHeaders:    []string{},
		AllowCredentials: false,
		MaxAge:           600,
	}
}

func CORS(cfg CORSConfig) zentrox.Handler {
	allowMethods := strings.Join(cfg.AllowMethods, ", ")
	allowHeaders := strings.Join(cfg.AllowHeaders, ", ")
	exposeHeaders := strings.Join(cfg.ExposeHeaders, ", ")
	maxAge := ""
	if cfg.MaxAge > 0 {
		maxAge = strings.TrimSpace(strconv.Itoa(cfg.MaxAge))
	}

	return func(c *zentrox.Context) {
		h := c.Writer.Header()

		// Handle Access-Control-Allow-Origin
		// If wildcard is in the list, use it; otherwise match origin against whitelist
		origin := c.GetHeader("Origin")
		acaoHeader := ""
		if len(cfg.AllowOrigins) > 0 {
			if cfg.AllowOrigins[0] == "*" {
				acaoHeader = "*"
			} else {
				// Check if origin matches any in the list
				for _, allowed := range cfg.AllowOrigins {
					if allowed == origin {
						acaoHeader = origin
						break
					}
				}
				// If no exact match and "*" is in the list, allow it
				if acaoHeader == "" {
					for _, allowed := range cfg.AllowOrigins {
						if allowed == "*" {
							acaoHeader = origin
							break
						}
					}
				}
			}
		}

		if acaoHeader != "" {
			h.Set("Access-Control-Allow-Origin", acaoHeader)
		}
		if allowMethods != "" {
			h.Set("Access-Control-Allow-Methods", allowMethods)
		}
		if allowHeaders != "" {
			h.Set("Access-Control-Allow-Headers", allowHeaders)
		}
		if exposeHeaders != "" {
			h.Set("Access-Control-Expose-Headers", exposeHeaders)
		}
		if cfg.AllowCredentials {
			h.Set("Access-Control-Allow-Credentials", "true")
		}
		if maxAge != "" {
			h.Set("Access-Control-Max-Age", maxAge)
		}

		if c.Request.Method == http.MethodOptions {
			// Use SendStatus to properly handle the response through the recorder
			c.SendStatus(http.StatusNoContent)
			c.Abort()
			return
		}
		c.Forward()
	}
}

// StrictCORS enforces safer CORS behavior on top of an existing CORS middleware.
// - If Allow-Credentials is true, it will not allow wildcard "*".
// - It can optionally restrict allowed origins to an exact-match whitelist.
type StrictCORSConfig struct {
	// If true, requests with no or unknown Origin are rejected when credentials are required.
	RequireKnownOrigin bool
	// Exact match list of allowed origins. Leave empty to allow any (but still not "*"
	// when credentials are used).
	AllowedOriginsExact []string
}

func StrictCORS(cfg StrictCORSConfig) zentrox.Handler {
	allowed := make(map[string]struct{}, len(cfg.AllowedOriginsExact))
	for _, o := range cfg.AllowedOriginsExact {
		allowed[strings.TrimSpace(o)] = struct{}{}
	}
	return func(c *zentrox.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		// Call next first so the underlying CORS middleware sets headers.
		c.Forward()

		h := c.Writer.Header()
		acao := h.Get("Access-Control-Allow-Origin")
		acredit := strings.EqualFold(h.Get("Access-Control-Allow-Credentials"), "true")

		// If credentials are allowed, wildcard is not permitted per spec/security.
		if acredit && acao == "*" {
			// Either reflect the exact origin if whitelisted, or drop origin to block credentialed cross-site.
			if origin == "" {
				if cfg.RequireKnownOrigin {
					// Block by clearing ACAO; browser will stop the response.
					h.Del("Access-Control-Allow-Origin")
				}
				return
			}
			if len(allowed) == 0 {
				// No whitelist provided: reflect the request origin.
				h.Set("Access-Control-Allow-Origin", origin)
				h.Add("Vary", "Origin")
				return
			}
			if _, ok := allowed[origin]; ok {
				h.Set("Access-Control-Allow-Origin", origin)
				h.Add("Vary", "Origin")
				return
			}
			// Not in whitelist: block by clearing ACAO.
			h.Del("Access-Control-Allow-Origin")
			return
		}

		// Always set Vary to avoid cache poisoning for CORS responses.
		if origin != "" {
			h.Add("Vary", "Origin")
		}
		if acr, ok := c.Request.Header["Access-Control-Request-Headers"]; ok && len(acr) > 0 {
			h.Add("Vary", "Access-Control-Request-Headers")
		}
		if acm := c.GetHeader("Access-Control-Request-Method"); acm != "" {
			h.Add("Vary", "Access-Control-Request-Method")
		}
	}
}
