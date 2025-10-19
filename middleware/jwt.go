package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aminofox/zentrox"
)

type JWTConfig struct {
	Secret      []byte
	AuthScheme  string // e.g., "Bearer"
	ContextKey  string // set claims under this key
	ValidateExp bool
	Leeway      int64 // seconds
}

func JWT(cfg JWTConfig) zentrox.Handler {
	if cfg.AuthScheme == "" {
		cfg.AuthScheme = "Bearer"
	}
	if cfg.ContextKey == "" {
		cfg.ContextKey = "user"
	}
	if !cfg.ValidateExp {
		cfg.ValidateExp = true
	}

	return func(c *zentrox.Context) {
		auth := c.GetHeader("Authorization")
		prefix := cfg.AuthScheme + " "
		if !strings.HasPrefix(auth, prefix) {
			c.SendJSON(http.StatusUnauthorized, map[string]any{"error": "missing authorization header"})
			return
		}
		token := strings.TrimPrefix(auth, prefix)
		claims, err := verifyHS256(token, cfg.Secret, cfg.ValidateExp, cfg.Leeway)
		if err != nil {
			c.SendJSON(http.StatusUnauthorized, map[string]any{"error": "invalid token"})
			return
		}
		c.Set(cfg.ContextKey, claims)
		c.Forward()
	}
}

func verifyHS256(token string, secret []byte, checkExp bool, leeway int64) (map[string]any, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("invalid token format")
	}

	// header
	hdrB, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, err
	}
	var hdr map[string]any
	if err := json.Unmarshal(hdrB, &hdr); err != nil {
		return nil, err
	}
	if hdr["alg"] != "HS256" {
		return nil, errors.New("unsupported alg")
	}

	// signature
	signing := parts[0] + "." + parts[1]
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signing))
	want := mac.Sum(nil)
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, err
	}
	if !hmac.Equal(got, want) {
		return nil, errors.New("signature mismatch")
	}

	// claims
	pldB, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, err
	}
	var claims map[string]any
	if err := json.Unmarshal(pldB, &claims); err != nil {
		return nil, err
	}

	if checkExp {
		now := time.Now().Unix()
		if v, ok := asInt64(claims["nbf"]); ok && now+leeway < v {
			return nil, errors.New("token not yet valid")
		}
		if v, ok := asInt64(claims["exp"]); ok && now-leeway > v {
			return nil, errors.New("token expired")
		}
	}
	return claims, nil
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

func asInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case int64:
		return t, true
	case int:
		return int64(t), true
	}
	return 0, false
}

// JWTChecks adds policy checks on top of an existing JWT authentication middleware.
// It validates header "alg" against a whitelist and enforces issuer/audience claims,
// as well as exp/nbf with an optional clock skew.
type JWTChecksConfig struct {
	AllowedAlgs []string      // e.g. []string{"RS256","ES256"}; empty means no alg check
	Issuer      string        // optional exact issuer match
	Audience    string        // optional exact audience match
	ClockSkew   time.Duration // default 60s
	// If true, missing exp/nbf are considered invalid.
	RequireExp bool
	RequireNbf bool
}

func JWTChecks(cfg JWTChecksConfig) zentrox.Handler {
	algOK := map[string]struct{}{}
	for _, a := range cfg.AllowedAlgs {
		algOK[strings.ToUpper(strings.TrimSpace(a))] = struct{}{}
	}
	if cfg.ClockSkew == 0 {
		cfg.ClockSkew = 60 * time.Second
	}

	return func(c *zentrox.Context) {
		const bearer = "Bearer "
		auth := c.GetHeader("Authorization")
		if !strings.HasPrefix(auth, bearer) {
			c.Forward()
			return
		}
		raw := strings.TrimSpace(auth[len(bearer):])
		parts := strings.Split(raw, ".")
		if len(parts) != 3 {
			c.Fail(http.StatusUnauthorized, "invalid token format")
			return
		}

		// Decode header
		hb, err := base64.RawURLEncoding.DecodeString(parts[0])
		if err != nil {
			c.Fail(http.StatusUnauthorized, "invalid token header")
			return
		}
		var hdr struct {
			Alg string `json:"alg"`
			Typ string `json:"typ"`
			Kid string `json:"kid"`
		}
		if err := json.Unmarshal(hb, &hdr); err != nil {
			c.Fail(http.StatusUnauthorized, "invalid token header json")
			return
		}
		if len(algOK) > 0 {
			if _, ok := algOK[strings.ToUpper(hdr.Alg)]; !ok {
				c.Fail(http.StatusUnauthorized, "disallowed jwt alg")
				return
			}
		}

		// Decode payload
		pb, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			c.Fail(http.StatusUnauthorized, "invalid token payload")
			return
		}
		var claims map[string]any
		if err := json.Unmarshal(pb, &claims); err != nil {
			c.Fail(http.StatusUnauthorized, "invalid token payload json")
			return
		}

		now := time.Now()
		// exp
		if v, ok := claims["exp"]; ok {
			if exp, ok := asUnix(v); ok {
				if now.After(exp.Add(cfg.ClockSkew)) {
					c.Fail(http.StatusUnauthorized, "token expired")
					return
				}
			} else if cfg.RequireExp {
				c.Fail(http.StatusUnauthorized, "missing exp")
				return
			}
		} else if cfg.RequireExp {
			c.Fail(http.StatusUnauthorized, "missing exp")
			return
		}
		// nbf
		if v, ok := claims["nbf"]; ok {
			if nbf, ok := asUnix(v); ok {
				if now.Add(cfg.ClockSkew).Before(nbf) {
					c.Fail(http.StatusUnauthorized, "token not yet valid")
					return
				}
			} else if cfg.RequireNbf {
				c.Fail(http.StatusUnauthorized, "missing nbf")
				return
			}
		} else if cfg.RequireNbf {
			c.Fail(http.StatusUnauthorized, "missing nbf")
			return
		}
		// iss
		if cfg.Issuer != "" {
			if iss, _ := claims["iss"].(string); iss != cfg.Issuer {
				c.Fail(http.StatusUnauthorized, "invalid issuer")
				return
			}
		}
		// aud (string or []string)
		if cfg.Audience != "" {
			switch v := claims["aud"].(type) {
			case string:
				if v != cfg.Audience {
					c.Fail(http.StatusUnauthorized, "invalid audience")
					return
				}
			case []any:
				match := false
				for _, it := range v {
					if s, _ := it.(string); s == cfg.Audience {
						match = true
						break
					}
				}
				if !match {
					c.Fail(http.StatusUnauthorized, "invalid audience")
					return
				}
			case []string:
				match := false
				for _, s := range v {
					if s == cfg.Audience {
						match = true
						break
					}
				}
				if !match {
					c.Fail(http.StatusUnauthorized, "invalid audience")
					return
				}
			default:
				// if audience is required but type unknown
				c.Fail(http.StatusUnauthorized, "invalid audience")
				return
			}
		}

		// All checks passed; proceed to your existing JWT verifier/handler.
		c.Forward()
	}
}

// asUnix parses exp/nbf numeric date (seconds) returning a time.Time.
func asUnix(v any) (time.Time, bool) {
	switch t := v.(type) {
	case float64:
		return time.Unix(int64(t), 0), true
	case json.Number:
		if n, err := t.Int64(); err == nil {
			return time.Unix(n, 0), true
		}
	case int64:
		return time.Unix(t, 0), true
	case int:
		return time.Unix(int64(t), 0), true
	}
	return time.Time{}, false
}
