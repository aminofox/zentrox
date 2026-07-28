package z_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aminofox/zentrox/v2"
	"github.com/aminofox/zentrox/v2/middleware"
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

func TestLoggerUsesRouteTemplate(t *testing.T) {
	app := zentrox.NewApp()
	var loggedPath string
	app.Plug(middleware.LoggerWithFunc(func(method, path string, status int, duration time.Duration, err error) {
		loggedPath = path
	}))
	app.GET("/users/:id", func(c *zentrox.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users/12345", nil))

	if loggedPath != "/users/:id" {
		t.Fatalf("logger path want route template, got %q", loggedPath)
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

func TestRateLimit_MaxKeysRejectsNewKeysWhenFull(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.RateLimit(middleware.RateLimitConfig{
		Rate:    100,
		Burst:   100,
		MaxKeys: 1,
		KeyFunc: func(c *zentrox.Context) string {
			return c.Query("key")
		},
	}))
	app.GET("/rl", func(c *zentrox.Context) {
		c.String(http.StatusOK, "ok")
	})

	w1 := httptest.NewRecorder()
	app.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/rl?key=a", nil))
	if w1.Code != http.StatusOK {
		t.Fatalf("first key want 200, got %d", w1.Code)
	}

	w2 := httptest.NewRecorder()
	app.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/rl?key=b", nil))
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("new key after cap want 429, got %d", w2.Code)
	}
}

func TestCORS_ImplicitPreflightUsesGlobalMiddleware(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.CORS(middleware.DefaultCORS()))
	app.GET("/cors", func(c *zentrox.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodOptions, "/cors", nil)
	req.Header.Set(zentrox.HeaderOrigin, "https://example.com")
	req.Header.Set(zentrox.HeaderAccessControlRequestMethod, http.MethodGet)
	req.Header.Set(zentrox.HeaderAccessControlRequestHeaders, "Authorization, X-Trace-ID")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight want 204, got %d", w.Code)
	}
	if got := w.Header().Get(zentrox.HeaderAccessControlAllowOrigin); got != "*" {
		t.Fatalf("allow-origin want *, got %q", got)
	}
	if got := w.Header().Get(zentrox.HeaderAccessControlAllowHeaders); got != "Authorization, X-Trace-ID" {
		t.Fatalf("allow-headers should reflect requested headers for wildcard config, got %q", got)
	}
	if got := w.Header().Get(zentrox.HeaderAllow); got != "GET, HEAD, OPTIONS" {
		t.Fatalf("Allow header want %q, got %q", "GET, HEAD, OPTIONS", got)
	}
}

func TestCORS_NoOriginDoesNotSetCORSHeaders(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.CORS(middleware.DefaultCORS()))
	app.GET("/cors", func(c *zentrox.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/cors", nil))

	if got := w.Header().Get(zentrox.HeaderAccessControlAllowOrigin); got != "" {
		t.Fatalf("non-CORS request should not get allow-origin, got %q", got)
	}
	if got := w.Header().Get(zentrox.HeaderVary); got != "" {
		t.Fatalf("non-CORS request should not get Vary, got %q", got)
	}
}

func TestCORS_WildcardWithCredentialsDoesNotReflectOrigin(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.CORS(middleware.CORSConfig{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{http.MethodGet},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: true,
	}))
	app.GET("/cors", func(c *zentrox.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/cors", nil)
	req.Header.Set(zentrox.HeaderOrigin, "https://example.com")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if got := w.Header().Get(zentrox.HeaderAccessControlAllowOrigin); got != "" {
		t.Fatalf("credentialed wildcard should not reflect arbitrary origin, got %q", got)
	}
	if got := w.Header().Get(zentrox.HeaderAccessControlAllowCredentials); got != "" {
		t.Fatalf("allow-credentials should not be set for rejected origin, got %q", got)
	}
	if got := w.Header().Get(zentrox.HeaderVary); !strings.Contains(got, zentrox.HeaderOrigin) {
		t.Fatalf("Vary should include Origin, got %q", got)
	}
}

func TestCORS_AllowOriginFuncWithCredentials(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.CORS(middleware.CORSConfig{
		AllowOriginFunc: func(origin string) bool {
			return strings.HasSuffix(origin, ".example.com")
		},
		AllowMethods:     []string{http.MethodGet},
		AllowHeaders:     []string{"Content-Type"},
		AllowCredentials: true,
	}))
	app.GET("/cors", func(c *zentrox.Context) {
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/cors", nil)
	req.Header.Set(zentrox.HeaderOrigin, "https://app.example.com")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if got := w.Header().Get(zentrox.HeaderAccessControlAllowOrigin); got != "https://app.example.com" {
		t.Fatalf("origin func should reflect allowed origin, got %q", got)
	}
	if got := w.Header().Get(zentrox.HeaderAccessControlAllowCredentials); got != "true" {
		t.Fatalf("allow-credentials want true, got %q", got)
	}
	if got := w.Header().Get(zentrox.HeaderVary); !strings.Contains(got, zentrox.HeaderOrigin) {
		t.Fatalf("Vary should include Origin, got %q", got)
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

func TestHTTPProtection_BlocksMethodAndLongURI(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.HTTPProtection(middleware.HTTPProtectionConfig{
		AllowedMethods: []string{http.MethodPost},
		MaxURLLength:   16,
	}))
	app.GET("/safe", func(c *zentrox.Context) {
		c.String(http.StatusOK, "ok")
	})

	w1 := httptest.NewRecorder()
	app.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/safe", nil))
	if w1.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method check want 405, got %d", w1.Code)
	}
	if got := w1.Header().Get(zentrox.HeaderAllow); got != http.MethodPost {
		t.Fatalf("allow header want %q, got %q", http.MethodPost, got)
	}

	w2 := httptest.NewRecorder()
	app.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/safe?query=abcdefghijklmnopqrstuvwxyz", nil))
	if w2.Code != http.StatusRequestURITooLong {
		t.Fatalf("uri length check want 414, got %d", w2.Code)
	}
}

func TestBodyLimit_BlocksLargePayload(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.BodyLimit(middleware.BodyLimitConfig{MaxBytes: 10}))

	called := false
	app.POST("/upload", func(c *zentrox.Context) {
		called = true
		var payload map[string]any
		_ = c.BindJSONInto(&payload)
	})

	body := `{"data":"abcdefghijklmnopqrstuvwxyz"}`
	req := httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader(body))
	req.Header.Set(zentrox.HeaderContentType, zentrox.ContentTypeJSON)
	req.ContentLength = -1

	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if !called {
		t.Fatal("handler should be called when body size is unknown until read")
	}
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", w.Code)
	}
}

func TestSecurityHeaders_Default(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.SecurityHeaders(middleware.DefaultSecurityHeaders()))
	app.GET("/ok", func(c *zentrox.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))

	if got := w.Header().Get(zentrox.HeaderXContentTypeOptions); got != "nosniff" {
		t.Fatalf("x-content-type-options want nosniff, got %q", got)
	}
	if got := w.Header().Get(zentrox.HeaderXFrameOptions); got != "DENY" {
		t.Fatalf("x-frame-options want DENY, got %q", got)
	}
	if got := w.Header().Get(zentrox.HeaderReferrerPolicy); got != "strict-origin-when-cross-origin" {
		t.Fatalf("referrer-policy unexpected: %q", got)
	}
}

func TestConcurrencyLimit_QueueTimeout(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.ConcurrencyLimit(middleware.ConcurrencyLimitConfig{
		MaxConcurrent: 1,
		QueueTimeout:  20 * time.Millisecond,
	}))

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	done := make(chan struct{})

	app.GET("/work", func(c *zentrox.Context) {
		entered <- struct{}{}
		<-release
		c.SendStatus(http.StatusOK)
	})

	firstW := httptest.NewRecorder()
	go func() {
		app.ServeHTTP(firstW, httptest.NewRequest(http.MethodGet, "/work", nil))
		close(done)
	}()

	select {
	case <-entered:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("first request did not enter handler")
	}

	secondW := httptest.NewRecorder()
	app.ServeHTTP(secondW, httptest.NewRequest(http.MethodGet, "/work", nil))
	if secondW.Code != http.StatusServiceUnavailable {
		t.Fatalf("second request want 503, got %d", secondW.Code)
	}

	close(release)
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("first request did not finish")
	}
	if firstW.Code != http.StatusOK {
		t.Fatalf("first request want 200, got %d", firstW.Code)
	}
}

func TestDefaultAPIHardening_Preset(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.DefaultAPIHardening()...)
	app.GET("/ok", func(c *zentrox.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if got := w.Header().Get(zentrox.XRequestID); got == "" {
		t.Fatal("request id should be set by preset")
	}
	if got := w.Header().Get(zentrox.HeaderXContentTypeOptions); got != "nosniff" {
		t.Fatalf("x-content-type-options want nosniff, got %q", got)
	}

	w2 := httptest.NewRecorder()
	app.ServeHTTP(w2, httptest.NewRequest(http.MethodTrace, "/ok", nil))
	if w2.Code != http.StatusMethodNotAllowed {
		t.Fatalf("trace should be blocked by preset, got %d", w2.Code)
	}
}

func TestDefaultAPIHardeningFast_Preset(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.DefaultAPIHardeningFast()...)
	app.GET("/ok", func(c *zentrox.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if got := w.Header().Get(zentrox.XRequestID); got != "" {
		t.Fatalf("fast preset should not inject request id, got %q", got)
	}
	if got := w.Header().Get(zentrox.HeaderXContentTypeOptions); got != "nosniff" {
		t.Fatalf("x-content-type-options want nosniff, got %q", got)
	}

	w2 := httptest.NewRecorder()
	app.ServeHTTP(w2, httptest.NewRequest(http.MethodTrace, "/ok", nil))
	if w2.Code != http.StatusMethodNotAllowed {
		t.Fatalf("trace should be blocked by fast preset, got %d", w2.Code)
	}
}
