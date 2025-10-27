package z_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
)

type httpErr struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Detail  interface{} `json:"detail"`
}

func TestErrorHandler_Panic(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.ErrorHandler(middleware.DefaultErrorHandler()))
	app.GET("/panic", func(c *zentrox.Context) { panic("boom") })

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
	var e httpErr
	_ = json.Unmarshal(w.Body.Bytes(), &e)
	if e.Code != 500 || e.Message == "" {
		t.Fatalf("unexpected error payload: %+v", e)
	}
}

func TestErrorHandler_Fail(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.ErrorHandler(middleware.DefaultErrorHandler()))
	app.GET("/bad", func(c *zentrox.Context) { c.Fail(http.StatusBadRequest, "bad req") })

	req := httptest.NewRequest(http.MethodGet, "/bad", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", w.Code)
	}
}
