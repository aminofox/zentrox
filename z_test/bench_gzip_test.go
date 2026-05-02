package z_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aminofox/zentrox/v2"
	"github.com/aminofox/zentrox/v2/middleware"
)

func BenchmarkGzip_BigJSON(b *testing.B) {
	app := zentrox.NewApp()
	app.Plug(middleware.Gzip())

	payload := "{\"data\":\"" + strings.Repeat("abcdef0123456789", 4096) + "\"}"
	app.GET("/json", func(c *zentrox.Context) {
		c.SetHeader(zentrox.HeaderContentType, zentrox.ContentTypeJSON)
		c.SendBytes(http.StatusOK, []byte(payload))
	})

	req := httptest.NewRequest(http.MethodGet, "/json", nil)
	req.Header.Set(zentrox.HeaderAcceptEncoding, "gzip")
	w := httptest.NewRecorder()

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		app.ServeHTTP(w, req)
		w.Body.Reset()
		w.Result().Body.Close()
	}
}
