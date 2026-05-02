package z_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/aminofox/zentrox/v2"
)

func TestOnResponseCapturesFinalStatus(t *testing.T) {
	app := zentrox.NewApp()

	gotStatus := 0
	gotLatency := time.Duration(0)
	app.SetOnResponse(func(_ *zentrox.Context, status int, latency time.Duration) {
		gotStatus = status
		gotLatency = latency
	})

	app.GET("/no-content", func(c *zentrox.Context) {
		c.SendStatus(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/no-content", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", w.Code)
	}
	if gotStatus != http.StatusNoContent {
		t.Fatalf("onResponse want status 204, got %d", gotStatus)
	}
	if gotLatency < 0 {
		t.Fatalf("latency should be >= 0, got %v", gotLatency)
	}
}

func TestScopeAutoOptions(t *testing.T) {
	app := zentrox.NewApp()
	api := app.Scope("/api")
	api.GET("/users", func(c *zentrox.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/users", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", w.Code)
	}
}

func TestStaticRootPathServesIndex(t *testing.T) {
	app := zentrox.NewApp()
	tmp := t.TempDir()

	indexPath := filepath.Join(tmp, "index.html")
	if err := os.WriteFile(indexPath, []byte("<h1>ok</h1>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	app.Static("/assets", zentrox.StaticOptions{Dir: tmp, Index: "index.html"})

	req := httptest.NewRequest(http.MethodGet, "/assets", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if got := w.Body.String(); got != "<h1>ok</h1>" {
		t.Fatalf("unexpected body: %q", got)
	}
}

func TestRealIPTrustedProxy(t *testing.T) {
	app := zentrox.NewApp()
	app.GET("/ip", func(c *zentrox.Context) {
		c.String(http.StatusOK, "%s", c.RealIP())
	})

	// Default: no trusted proxy => ignore X-Forwarded-For
	req := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set(zentrox.HeaderXForwardedFor, "203.0.113.10")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if got := w.Body.String(); got != "10.0.0.1" {
		t.Fatalf("want remote ip, got %q", got)
	}

	// Trust 10.0.0.0/8 => use first untrusted from XFF chain
	app.SetTrustedProxies("10.0.0.0/8")
	req2 := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req2.RemoteAddr = "10.0.0.1:1234"
	req2.Header.Set(zentrox.HeaderXForwardedFor, "203.0.113.10, 10.1.1.1")
	w2 := httptest.NewRecorder()
	app.ServeHTTP(w2, req2)
	if got := w2.Body.String(); got != "203.0.113.10" {
		t.Fatalf("want client ip from XFF, got %q", got)
	}
}
