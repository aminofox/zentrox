package z_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
)

func benchMiddlewareCostGet(b *testing.B, plugs ...zentrox.Handler) {
	app := zentrox.NewApp()
	if len(plugs) > 0 {
		app.Plug(plugs...)
	}
	app.GET("/cost", func(c *zentrox.Context) { c.SendStatus(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodGet, "/cost", nil)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.ServeHTTP(discardRW{rec}, req)
	}
}

func BenchmarkMiddlewareCost_Baseline(b *testing.B) {
	benchMiddlewareCostGet(b)
}

func BenchmarkMiddlewareCost_SecurityHeaders(b *testing.B) {
	benchMiddlewareCostGet(b, middleware.SecurityHeaders(middleware.DefaultSecurityHeaders()))
}

func BenchmarkMiddlewareCost_HTTPProtection(b *testing.B) {
	benchMiddlewareCostGet(b, middleware.HTTPProtection(middleware.DefaultHTTPProtection()))
}

func BenchmarkMiddlewareCost_BodyLimit_NoBody(b *testing.B) {
	benchMiddlewareCostGet(b, middleware.BodyLimit(middleware.DefaultBodyLimit()))
}

func BenchmarkMiddlewareCost_ConcurrencyLimit(b *testing.B) {
	benchMiddlewareCostGet(b, middleware.ConcurrencyLimit(middleware.DefaultConcurrencyLimit()))
}

func BenchmarkMiddlewareCost_RateLimit(b *testing.B) {
	benchMiddlewareCostGet(b, middleware.RateLimit(middleware.RateLimitConfig{
		Rate:  1000000,
		Burst: 1000000,
		KeyFunc: func(*zentrox.Context) string {
			return "bench"
		},
		StaleAfter: time.Hour,
	}))
}

func BenchmarkMiddlewareCost_Timeout(b *testing.B) {
	benchMiddlewareCostGet(b, middleware.Timeout(2*time.Second))
}

func BenchmarkMiddlewareCost_DefaultAPIHardening(b *testing.B) {
	benchMiddlewareCostGet(b, middleware.DefaultAPIHardening()...)
}

func BenchmarkMiddlewareCost_DefaultAPIHardeningFast(b *testing.B) {
	benchMiddlewareCostGet(b, middleware.DefaultAPIHardeningFast()...)
}

func BenchmarkMiddlewareCost_BodyLimit_ReadSmallJSON(b *testing.B) {
	app := zentrox.NewApp()
	app.Plug(middleware.BodyLimit(middleware.DefaultBodyLimit()))
	app.POST("/cost", func(c *zentrox.Context) {
		var payload map[string]any
		_ = c.BindJSONInto(&payload)
		c.SendStatus(http.StatusNoContent)
	})

	body := `{"ok":true,"n":123}`

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/cost", strings.NewReader(body))
		req.Header.Set(zentrox.HeaderContentType, zentrox.ContentTypeJSON)
		rec := httptest.NewRecorder()
		app.ServeHTTP(discardRW{rec}, req)
	}
}
