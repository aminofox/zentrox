package middleware

import (
	"net/http"
	"sort"
	"strings"

	"github.com/aminofox/zentrox/v2"
)

type HTTPProtectionConfig struct {
	AllowedMethods     []string
	MaxURLLength       int
	OnMethodNotAllowed func(*zentrox.Context)
	OnURITooLong       func(*zentrox.Context)
}

func DefaultHTTPProtection() HTTPProtectionConfig {
	return HTTPProtectionConfig{
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodHead,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodOptions,
		},
		MaxURLLength: 2048,
		OnMethodNotAllowed: func(c *zentrox.Context) {
			c.Fail(http.StatusMethodNotAllowed, zentrox.MsgMethodNotAllowed)
		},
		OnURITooLong: func(c *zentrox.Context) {
			c.Fail(http.StatusRequestURITooLong, zentrox.MsgURITooLong)
		},
	}
}

func HTTPProtection(cfg HTTPProtectionConfig) zentrox.Handler {
	if cfg.OnMethodNotAllowed == nil {
		cfg.OnMethodNotAllowed = func(c *zentrox.Context) {
			c.Fail(http.StatusMethodNotAllowed, zentrox.MsgMethodNotAllowed)
		}
	}
	if cfg.OnURITooLong == nil {
		cfg.OnURITooLong = func(c *zentrox.Context) {
			c.Fail(http.StatusRequestURITooLong, zentrox.MsgURITooLong)
		}
	}
	if cfg.MaxURLLength < 0 {
		cfg.MaxURLLength = 0
	}

	allowMap := normalizeMethodSet(cfg.AllowedMethods)
	allowHeader := strings.Join(sortedMethods(allowMap), ", ")

	return func(c *zentrox.Context) {
		if cfg.MaxURLLength > 0 {
			target := c.Request.RequestURI
			if target == "" {
				target = c.Request.URL.RequestURI()
			}
			if len(target) > cfg.MaxURLLength {
				cfg.OnURITooLong(c)
				c.Abort()
				return
			}
		}

		if len(allowMap) > 0 {
			method := c.Request.Method
			if !allowMap[method] {
				method = strings.ToUpper(method)
				if !allowMap[method] {
					if allowHeader != "" {
						c.SetHeader(zentrox.HeaderAllow, allowHeader)
					}
					cfg.OnMethodNotAllowed(c)
					c.Abort()
					return
				}
			}
		}

		c.Next()
	}
}

func normalizeMethodSet(methods []string) map[string]bool {
	if len(methods) == 0 {
		return nil
	}
	out := make(map[string]bool, len(methods))
	for _, m := range methods {
		m = strings.ToUpper(strings.TrimSpace(m))
		if m == "" {
			continue
		}
		out[m] = true
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func sortedMethods(methods map[string]bool) []string {
	if len(methods) == 0 {
		return nil
	}
	out := make([]string, 0, len(methods))
	for m := range methods {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}
