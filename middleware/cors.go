package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/aminofox/zentrox/v2"
)

type CORSConfig struct {
	AllowOrigins     []string
	AllowOriginFunc  func(origin string) bool
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           int
}

func DefaultCORS() CORSConfig {
	return CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"*"},
		ExposeHeaders:    []string{},
		AllowCredentials: false,
		MaxAge:           3600,
	}
}

func CORS(cfg CORSConfig) zentrox.Handler {
	allowMethods := strings.Join(cfg.AllowMethods, ", ")
	allowHeaders := strings.Join(cfg.AllowHeaders, ", ")
	exposeHeaders := strings.Join(cfg.ExposeHeaders, ", ")
	maxAge := strconv.Itoa(cfg.MaxAge)

	allowMap := make(map[string]bool)
	hasWildcard := false
	for _, o := range cfg.AllowOrigins {
		if o == "*" {
			hasWildcard = true
		}
		allowMap[o] = true
	}

	return func(c *zentrox.Context) {
		origin := c.GetHeader(zentrox.HeaderOrigin)
		h := c.Writer.Header()
		if origin == "" {
			c.Next()
			return
		}

		acao := ""
		if hasWildcard && !cfg.AllowCredentials {
			acao = "*"
		} else if cfg.AllowOriginFunc != nil && cfg.AllowOriginFunc(origin) {
			acao = origin
		} else if allowMap[origin] {
			acao = origin
		}

		if acao != "*" {
			addVary(h, zentrox.HeaderOrigin)
		}

		if acao != "" {
			h.Set(zentrox.HeaderAccessControlAllowOrigin, acao)

			if allowMethods != "" {
				h.Set(zentrox.HeaderAccessControlAllowMethods, allowMethods)
			}
			if allowHeaders != "" {
				if strings.TrimSpace(allowHeaders) == "*" {
					if requested := c.GetHeader(zentrox.HeaderAccessControlRequestHeaders); requested != "" {
						h.Set(zentrox.HeaderAccessControlAllowHeaders, requested)
						addVary(h, zentrox.HeaderAccessControlRequestHeaders)
					} else {
						h.Set(zentrox.HeaderAccessControlAllowHeaders, allowHeaders)
					}
				} else {
					h.Set(zentrox.HeaderAccessControlAllowHeaders, allowHeaders)
				}
			}
			if exposeHeaders != "" {
				h.Set(zentrox.HeaderAccessControlExposeHeaders, exposeHeaders)
			}
			if cfg.AllowCredentials {
				h.Set(zentrox.HeaderAccessControlAllowCredentials, "true")
			}
			if cfg.MaxAge > 0 {
				h.Set(zentrox.HeaderAccessControlMaxAge, maxAge)
			}
		}

		if c.Request.Method == http.MethodOptions && c.GetHeader(zentrox.HeaderAccessControlRequestMethod) != "" {
			addVary(h, zentrox.HeaderAccessControlRequestMethod)
			c.SendStatus(http.StatusNoContent)
			c.Abort()
			return
		}

		c.Next()
	}
}

func addVary(h http.Header, value string) {
	for _, existing := range h.Values(zentrox.HeaderVary) {
		for _, part := range strings.Split(existing, ",") {
			if strings.EqualFold(strings.TrimSpace(part), value) {
				return
			}
		}
	}
	h.Add(zentrox.HeaderVary, value)
}
