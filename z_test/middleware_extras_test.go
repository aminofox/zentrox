package z_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
)

func TestRequestID_GenerateAndReuse(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.RequestID(middleware.DefaultRequestID()))
	app.GET("/id", func(c *zentrox.Context) {
		c.String(http.StatusOK, "%s", c.RequestID())
	})

	// generated
	req := httptest.NewRequest(http.MethodGet, "/id", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if got := w.Body.String(); got == "" {
		t.Fatal("request id should be generated")
	}
	if got := w.Header().Get(zentrox.XRequestID); got == "" {
		t.Fatal("response header should include request id")
	}

	// incoming value is reused
	req2 := httptest.NewRequest(http.MethodGet, "/id", nil)
	req2.Header.Set(zentrox.XRequestID, "req-123")
	w2 := httptest.NewRecorder()
	app.ServeHTTP(w2, req2)
	if got := w2.Body.String(); got != "req-123" {
		t.Fatalf("want reused request id, got %q", got)
	}
}

func TestRateLimit_Basic(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.RateLimit(middleware.RateLimitConfig{
		Rate:  1,
		Burst: 1,
		KeyFunc: func(*zentrox.Context) string {
			return "k"
		},
	}))
	app.GET("/rl", func(c *zentrox.Context) {
		c.String(http.StatusOK, "ok")
	})

	w1 := httptest.NewRecorder()
	app.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/rl", nil))
	if w1.Code != http.StatusOK {
		t.Fatalf("first request want 200, got %d", w1.Code)
	}

	w2 := httptest.NewRecorder()
	app.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/rl", nil))
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request want 429, got %d", w2.Code)
	}
}

func TestTimeoutMiddleware(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.Timeout(20 * time.Millisecond))
	app.GET("/slow", func(c *zentrox.Context) {
		<-c.Done()
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/slow", nil))

	if w.Code != http.StatusGatewayTimeout {
		t.Fatalf("want 504, got %d", w.Code)
	}
}
