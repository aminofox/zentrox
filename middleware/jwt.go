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

	"github.com/aminofox/zentrox/v2"
)

type RegisteredClaims struct {
	Issuer    string   `json:"iss,omitempty"`
	Subject   string   `json:"sub,omitempty"`
	Audience  string   `json:"aud,omitempty"`
	Audiences []string `json:"-"`
	ExpiresAt int64    `json:"exp,omitempty"`
	NotBefore int64    `json:"nbf,omitempty"`
	IssuedAt  int64    `json:"iat,omitempty"`
	ID        string   `json:"jti,omitempty"`
}

type JWTClaims struct {
	RegisteredClaims
	Custom map[string]any `json:"-"`
}

func (c *JWTClaims) UnmarshalJSON(data []byte) error {
	var rc struct {
		Issuer    string          `json:"iss,omitempty"`
		Subject   string          `json:"sub,omitempty"`
		Audience  json.RawMessage `json:"aud,omitempty"`
		ExpiresAt int64           `json:"exp,omitempty"`
		NotBefore int64           `json:"nbf,omitempty"`
		IssuedAt  int64           `json:"iat,omitempty"`
		ID        string          `json:"jti,omitempty"`
	}
	if err := json.Unmarshal(data, &rc); err != nil {
		return err
	}
	c.RegisteredClaims = RegisteredClaims{
		Issuer:    rc.Issuer,
		Subject:   rc.Subject,
		ExpiresAt: rc.ExpiresAt,
		NotBefore: rc.NotBefore,
		IssuedAt:  rc.IssuedAt,
		ID:        rc.ID,
	}
	if len(rc.Audience) > 0 {
		var single string
		if err := json.Unmarshal(rc.Audience, &single); err == nil {
			c.Audience = single
			c.Audiences = []string{single}
		} else {
			var list []string
			if err := json.Unmarshal(rc.Audience, &list); err != nil {
				return errors.New("invalid audience claim")
			}
			c.Audiences = list
			if len(list) > 0 {
				c.Audience = list[0]
			}
		}
	}
	if err := json.Unmarshal(data, &c.Custom); err != nil {
		return err
	}
	delete(c.Custom, "iss")
	delete(c.Custom, "sub")
	delete(c.Custom, "aud")
	delete(c.Custom, "exp")
	delete(c.Custom, "nbf")
	delete(c.Custom, "iat")
	delete(c.Custom, "jti")
	return nil
}

func (c *JWTClaims) Valid(expectedIss, expectedAud string, clockSkew time.Duration) error {
	now := time.Now()
	skew := int64(clockSkew.Seconds())
	unixNow := now.Unix()
	if c.ExpiresAt > 0 && unixNow > c.ExpiresAt+skew {
		return errors.New("token is expired")
	}
	if c.NotBefore > 0 && unixNow+skew < c.NotBefore {
		return errors.New("token is not yet valid")
	}
	if c.IssuedAt > 0 && unixNow+skew < c.IssuedAt {
		return errors.New("token issued in the future")
	}
	if expectedIss != "" && c.Issuer != expectedIss {
		return errors.New("invalid issuer")
	}
	if expectedAud != "" && !c.hasAudience(expectedAud) {
		return errors.New("invalid audience")
	}
	return nil
}

func (c *JWTClaims) hasAudience(expected string) bool {
	if c.Audience == expected {
		return true
	}
	for _, aud := range c.Audiences {
		if aud == expected {
			return true
		}
	}
	return false
}

type JWTConfig struct {
	Secret        []byte
	ContextKey    string
	SkipIfMissing bool
	Algorithms    []string // Allowlist of algorithms, defaults to []string{"HS256"}
	Issuer        string   // Expected Issuer
	Audience      string   // Expected Audience
	ClockSkew     time.Duration
	ValidateFunc  func(claims *JWTClaims) error
}

func JWT(cfg JWTConfig) zentrox.Handler {
	if len(cfg.Secret) == 0 {
		panic("JWT: Secret is required")
	}
	if cfg.ContextKey == "" {
		cfg.ContextKey = "user"
	}
	if len(cfg.Algorithms) == 0 {
		cfg.Algorithms = []string{"HS256"}
	}
	for _, alg := range cfg.Algorithms {
		if alg != "HS256" {
			panic("JWT: unsupported algorithm " + alg)
		}
	}

	return func(c *zentrox.Context) {
		if len(c.Request.Header.Values(zentrox.HeaderAuthorization)) > 1 {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": zentrox.MsgInvalidToken})
			c.Abort()
			return
		}

		auth := c.GetHeader(zentrox.HeaderAuthorization)
		if !strings.HasPrefix(auth, zentrox.BearerPrefix) {
			if cfg.SkipIfMissing {
				c.Next()
				return
			}
			c.JSON(http.StatusUnauthorized, map[string]string{"error": zentrox.MsgMissingToken})
			c.Abort()
			return
		}

		token := strings.TrimPrefix(auth, zentrox.BearerPrefix)
		parts := strings.Split(token, ".")
		if len(parts) != 3 {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": zentrox.MsgInvalidToken})
			c.Abort()
			return
		}

		hb, err := base64.RawURLEncoding.DecodeString(parts[0])
		if err != nil {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": zentrox.MsgInvalidToken})
			c.Abort()
			return
		}

		var hdr struct {
			Alg string `json:"alg"`
		}
		if err := json.Unmarshal(hb, &hdr); err != nil {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": zentrox.MsgInvalidToken})
			c.Abort()
			return
		}

		algAllowed := false
		for _, a := range cfg.Algorithms {
			if a == hdr.Alg {
				algAllowed = true
				break
			}
		}

		if !algAllowed {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": zentrox.MsgUnsupportedAlg})
			c.Abort()
			return
		}

		signing := parts[0] + "." + parts[1]
		mac := hmac.New(sha256.New, cfg.Secret)
		mac.Write([]byte(signing))
		want := mac.Sum(nil)
		got, err := base64.RawURLEncoding.DecodeString(parts[2])
		if err != nil || !hmac.Equal(got, want) {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": zentrox.MsgInvalidSignature})
			c.Abort()
			return
		}

		pb, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": zentrox.MsgInvalidToken})
			c.Abort()
			return
		}

		var claims JWTClaims
		if err := json.Unmarshal(pb, &claims); err != nil {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": zentrox.MsgInvalidToken})
			c.Abort()
			return
		}

		if err := claims.Valid(cfg.Issuer, cfg.Audience, cfg.ClockSkew); err != nil {
			c.JSON(http.StatusUnauthorized, map[string]string{"error": err.Error()})
			c.Abort()
			return
		}

		if cfg.ValidateFunc != nil {
			if err := cfg.ValidateFunc(&claims); err != nil {
				c.JSON(http.StatusUnauthorized, map[string]string{"error": err.Error()})
				c.Abort()
				return
			}
		}

		c.Set(cfg.ContextKey, &claims)
		c.Next()
	}
}

func SignHS256(claims *JWTClaims, secret []byte) (string, error) {
	header := map[string]any{"alg": "HS256", "typ": "JWT"}
	hb, _ := json.Marshal(header)

	// Create a single map for marshalling back
	out := make(map[string]any)
	for k, v := range claims.Custom {
		out[k] = v
	}
	if claims.Issuer != "" {
		out["iss"] = claims.Issuer
	}
	if claims.Subject != "" {
		out["sub"] = claims.Subject
	}
	if len(claims.Audiences) > 1 {
		out["aud"] = claims.Audiences
	} else if claims.Audience != "" {
		out["aud"] = claims.Audience
	} else if len(claims.Audiences) == 1 {
		out["aud"] = claims.Audiences[0]
	}
	if claims.ExpiresAt > 0 {
		out["exp"] = claims.ExpiresAt
	}
	if claims.NotBefore > 0 {
		out["nbf"] = claims.NotBefore
	}
	if claims.IssuedAt > 0 {
		out["iat"] = claims.IssuedAt
	}
	if claims.ID != "" {
		out["jti"] = claims.ID
	}

	pb, _ := json.Marshal(out)
	h64 := base64.RawURLEncoding.EncodeToString(hb)
	p64 := base64.RawURLEncoding.EncodeToString(pb)
	signing := h64 + "." + p64
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(signing))
	sig := mac.Sum(nil)
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
