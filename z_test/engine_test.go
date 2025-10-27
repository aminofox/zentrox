package z_test

import (
	"net/http/httptest"
	"testing"

	"github.com/aminofox/zentrox"
)

func TestBasic(t *testing.T) {
	app := zentrox.NewApp()
	app.GET("/hi", func(c *zentrox.Context) { c.String(200, "hi") })

	req := httptest.NewRequest("GET", "/hi", nil)
	w := httptest.NewRecorder()

	app.ServeHTTP(w, req)

	if w.Code != 200 || w.Body.String() != "hi" {
		t.Fatalf("unexpected: %d %q", w.Code, w.Body.String())
	}
}
