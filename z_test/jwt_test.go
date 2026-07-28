package z_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aminofox/zentrox/v2"
	"github.com/aminofox/zentrox/v2/middleware"
)

func TestJWT_MissingHeader(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.JWT(middleware.JWTConfig{Secret: []byte("s")}))
	app.GET("/p", func(c *zentrox.Context) { c.String(200, "ok") })

	req := httptest.NewRequest(http.MethodGet, "/p", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestJWT_ValidToken(t *testing.T) {
	secret := []byte("s3cr3t")
	app := zentrox.NewApp()
	app.Plug(middleware.JWT(middleware.JWTConfig{Secret: secret}))
	app.GET("/me", func(c *zentrox.Context) {
		if _, ok := c.Get("user"); !ok {
			c.Fail(500, "no user in context", "")
			return
		}
		c.String(200, "ok")
	})

	claims := &middleware.JWTClaims{
		RegisteredClaims: middleware.RegisteredClaims{
			Subject:   "u1",
			ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
		},
	}
	tok, _ := middleware.SignHS256(claims, secret)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set(zentrox.HeaderAuthorization, zentrox.BearerPrefix+tok)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("want 200, got %d (body=%s)", w.Code, w.Body.String())
	}
}

func TestJWT_ExpiredToken(t *testing.T) {
	secret := []byte("s3cr3t")
	app := zentrox.NewApp()
	app.Plug(middleware.JWT(middleware.JWTConfig{
		Secret: secret,
		ValidateFunc: func(claims *middleware.JWTClaims) error {
			// This is now redundant since the middleware checks ExpiresAt internally
			// But we'll test the ValidateFunc anyway
			if claims.ExpiresAt < time.Now().Unix() {
				return errors.New("token expired logic test")
			}
			return nil
		},
	}))
	app.GET("/me", func(c *zentrox.Context) { c.String(200, "ok") })

	claims := &middleware.JWTClaims{
		RegisteredClaims: middleware.RegisteredClaims{
			Subject:   "u1",
			ExpiresAt: time.Now().Add(-1 * time.Hour).Unix(),
		},
	}
	tok, _ := middleware.SignHS256(claims, secret)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set(zentrox.HeaderAuthorization, zentrox.BearerPrefix+tok)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestJWT_AudienceArray(t *testing.T) {
	secret := []byte("s3cr3t")
	app := zentrox.NewApp()
	app.Plug(middleware.JWT(middleware.JWTConfig{Secret: secret, Audience: "api"}))
	app.GET("/me", func(c *zentrox.Context) { c.String(200, "ok") })

	claims := &middleware.JWTClaims{
		RegisteredClaims: middleware.RegisteredClaims{
			Subject:   "u1",
			Audiences: []string{"mobile", "api"},
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
		},
	}
	tok, _ := middleware.SignHS256(claims, secret)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set(zentrox.HeaderAuthorization, zentrox.BearerPrefix+tok)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestJWT_IssuedAtFutureWithClockSkew(t *testing.T) {
	secret := []byte("s3cr3t")
	claims := &middleware.JWTClaims{
		RegisteredClaims: middleware.RegisteredClaims{
			Subject:   "u1",
			IssuedAt:  time.Now().Add(30 * time.Second).Unix(),
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
		},
	}
	tok, _ := middleware.SignHS256(claims, secret)

	app := zentrox.NewApp()
	app.Plug(middleware.JWT(middleware.JWTConfig{Secret: secret}))
	app.GET("/me", func(c *zentrox.Context) { c.String(200, "ok") })
	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set(zentrox.HeaderAuthorization, zentrox.BearerPrefix+tok)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("future iat without skew want 401, got %d", w.Code)
	}

	app = zentrox.NewApp()
	app.Plug(middleware.JWT(middleware.JWTConfig{Secret: secret, ClockSkew: time.Minute}))
	app.GET("/me", func(c *zentrox.Context) { c.String(200, "ok") })
	w = httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("future iat within skew want 200, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestJWT_UnsupportedAlgorithmConfigPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("unsupported built-in JWT algorithm should panic")
		}
	}()
	_ = middleware.JWT(middleware.JWTConfig{Secret: []byte("s3cr3t"), Algorithms: []string{"RS256"}})
}

func TestJWT_EmptySecretConfigPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("empty JWT secret should panic")
		}
	}()
	_ = middleware.JWT(middleware.JWTConfig{})
}

func TestJWT_RejectsMultipleAuthorizationHeaders(t *testing.T) {
	secret := []byte("s3cr3t")
	app := zentrox.NewApp()
	app.Plug(middleware.JWT(middleware.JWTConfig{Secret: secret}))
	app.GET("/me", func(c *zentrox.Context) { c.String(200, "ok") })

	claims := &middleware.JWTClaims{
		RegisteredClaims: middleware.RegisteredClaims{
			Subject:   "u1",
			ExpiresAt: time.Now().Add(time.Hour).Unix(),
		},
	}
	tok, _ := middleware.SignHS256(claims, secret)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Add(zentrox.HeaderAuthorization, zentrox.BearerPrefix+tok)
	req.Header.Add(zentrox.HeaderAuthorization, zentrox.BearerPrefix+"other")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("multiple Authorization headers want 401, got %d", w.Code)
	}
}
