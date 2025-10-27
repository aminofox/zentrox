package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/aminofox/zentrox"
)

type JWTConfig struct {
	Secret        []byte
	ContextKey    string
	SkipIfMissing bool
	ValidateFunc  func(claims map[string]any) error
}

func JWT(cfg JWTConfig) zentrox.Handler {
	if cfg.ContextKey == "" {
		cfg.ContextKey = "user"
	}

	return func(c *zentrox.Context) {
		auth := c.GetHeader("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			if cfg.SkipIfMissing {
				c.Next()
				return
			}
			c.JSON(http.StatusUnauthorized, map[string]string{"error": "missing token"})
			c.Abort()
			return
		}

		token := strings.TrimPrefix(auth, "Bearer ")
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			c.Abort()
			return
		}

		hb, err := base64.RawURLEncoding.DecodeString(parts[0])
		if err != nil {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			c.Abort()
			return
		}

		var hdr struct {
			Alg string `json:"alg"`
		}
		if err := json.Unmarshal(hb, &hdr); err != nil {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			c.Abort()
			return
		}

		if hdr.Alg != "HS256" {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": "unsupported algorithm"})
			c.Abort()
			return
		}

		signing := parts[0] + "." + parts[1]
		mac := hmac.New(sha256.New, cfg.Secret)
		mac.Write([]byte(signing))
		want := mac.Sum(nil)
		got, err := base64.RawURLEncoding.DecodeString(parts[2])
		if err != nil || !hmac.Equal(got, want) {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid signature"})
			c.Abort()
			return
		}

		pb, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			c.Abort()
			return
		}

		var claims map[string]any
		if err := json.Unmarshal(pb, &claims); err != nil {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			c.Abort()
			return
		}

		if cfg.ValidateFunc != nil {
			if err := cfg.ValidateFunc(claims); err != nil {
				c.JSON(http.StatusUnauthorized, map[string]string{"error": err.Error()})
				c.Abort()
				return
			}
		}

		c.Set(cfg.ContextKey, claims)
		c.Next()
	}
}

func SignHS256(claims map[string]any, secret []byte) (string, error) {
	header := map[string]any{"alg": "HS256", "typ": "JWT"}
	hb, _ := json.Marshal(header)
	pb, _ := json.Marshal(claims)
	h64 := base64.RawURLEncoding.EncodeToString(hb)
	p64 := base64.RawURLEncoding.EncodeToString(pb)
	signing := h64 + "." + p64
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signing))
	sig := mac.Sum(nil)
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
