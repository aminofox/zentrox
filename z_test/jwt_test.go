package z_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
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
			c.Fail(500, "no user in context")
			return
		}
		c.String(200, "ok")
	})

	claims := map[string]any{"sub": "u1", "exp": time.Now().Add(1 * time.Hour).Unix()}
	tok, _ := middleware.SignHS256(claims, secret)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
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
		ValidateFunc: func(claims map[string]any) error {
			if exp, ok := claims["exp"].(float64); ok {
				if time.Now().Unix() > int64(exp) {
					return http.ErrAbortHandler // token expired
				}
			}
			return nil
		},
	}))
	app.GET("/me", func(c *zentrox.Context) { c.String(200, "ok") })

	claims := map[string]any{"sub": "u1", "exp": time.Now().Add(-1 * time.Hour).Unix()}
	tok, _ := middleware.SignHS256(claims, secret)

	req := httptest.NewRequest(http.MethodGet, "/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}
