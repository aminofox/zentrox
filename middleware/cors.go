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
		origin := c.GetHeader("Origin")
		h := c.Writer.Header()

		if origin == "" {
			origin = "*"
		}

		acao := ""
		if hasWildcard && !cfg.AllowCredentials {
			acao = "*"
		} else if allowMap[origin] {
			acao = origin
		} else if hasWildcard {
			acao = origin
		}

		if acao != "" {
			h.Set("Access-Control-Allow-Origin", acao)
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
		if cfg.MaxAge > 0 {
			h.Set("Access-Control-Max-Age", maxAge)
		}

		h.Add("Vary", "Origin")

		if c.Request.Method == http.MethodOptions {
			c.SendStatus(http.StatusNoContent)
			c.Abort()
			return
		}

		c.Next()
	}
}
