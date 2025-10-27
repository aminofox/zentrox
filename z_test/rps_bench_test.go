package z_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
)

// discardRW remove body to benchmark focusing on CPU processing of stack
type discardRW struct{ http.ResponseWriter }

func (d discardRW) Write(p []byte) (int, error) { return len(p), nil }

// newAppCommon create app with "basic" chain (no IO log for stable result)
func newAppCommon() *zentrox.App {
	app := zentrox.NewApp()
	// ErrorHandler is a common, lightweight chain; avoid AccessLog as writing IO will dirty the benchmark
	app.Plug(
		middleware.ErrorHandler(middleware.DefaultErrorHandler()),
	)
	return app
}

func benchRPS(b *testing.B, app *zentrox.App, req *http.Request) {
	b.ReportAllocs()
	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.ServeHTTP(discardRW{rec}, req)
	}
	elapsed := time.Since(start)
	b.StopTimer()
	// RPS = number of requests processed / measurement time
	rps := float64(b.N) / elapsed.Seconds()
	b.ReportMetric(rps, "rps")
}

func benchRPSParallel(b *testing.B, app *zentrox.App, req *http.Request) {
	b.ReportAllocs()
	var n int64
	b.ResetTimer()
	start := time.Now()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			rec := httptest.NewRecorder()
			app.ServeHTTP(discardRW{rec}, req)
			atomic.AddInt64(&n, 1)
		}
	})
	elapsed := time.Since(start)
	b.StopTimer()
	rps := float64(n) / elapsed.Seconds()
	b.ReportMetric(rps, "rps")
}

// Bench: Static route (very light handler)
func BenchmarkRPS_Static(b *testing.B) {
	app := newAppCommon()
	app.GET("/hi", func(c *zentrox.Context) {
		c.SendStatus(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/hi", nil)
	benchRPS(b, app, req)
}

// Bench: Param route (:id + wildcard)
func BenchmarkRPS_Param(b *testing.B) {
	app := newAppCommon()
	app.GET("/users/:id/files/*path", func(c *zentrox.Context) {
		_ = c.Param("id")
		_ = c.Param("path")
		c.SendStatus(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/users/42/files/a/b/c.txt", nil)
	benchRPS(b, app, req)
}

// Bench: Small JSON (return JSON small)
func BenchmarkRPS_SmallJSON(b *testing.B) {
	app := newAppCommon()
	app.GET("/json", func(c *zentrox.Context) {
		c.JSON(http.StatusOK, map[string]any{"ok": true, "n": 123})
	})

	req := httptest.NewRequest(http.MethodGet, "/json", nil)
	benchRPS(b, app, req)
}

// Bench: Small JSON (Parallel)
func BenchmarkRPS_SmallJSON_Parallel(b *testing.B) {
	app := newAppCommon()
	app.GET("/json", func(c *zentrox.Context) {
		c.JSON(http.StatusOK, map[string]any{"ok": true, "n": 123})
	})

	req := httptest.NewRequest(http.MethodGet, "/json", nil)
	benchRPSParallel(b, app, req)
}
