package z_test

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/aminofox/zentrox"
)

func benchmarkServe(b *testing.B, app *zentrox.App, path string, method string) {
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		app.ServeHTTP(w, req)
	}
}

func BenchmarkRouter_Static(b *testing.B) {
	app := zentrox.NewApp()
	for i := 0; i < 1000; i++ {
		p := "/s" + strconv.Itoa(i)
		app.GET(p, func(c *zentrox.Context) {
			c.String(204, "ok")
		})
	}
	benchmarkServe(b, app, "/s500", http.MethodGet)
}

func BenchmarkRouter_Param(b *testing.B) {
	app := zentrox.NewApp()
	for i := 0; i < 1000; i++ {
		p := "/u/:id/p/" + strconv.Itoa(i)
		app.GET(p, func(c *zentrox.Context) { _ = c.Param("id"); c.String(204, "ok") })
	}
	benchmarkServe(b, app, "/u/12345/p/777", http.MethodGet)
}
