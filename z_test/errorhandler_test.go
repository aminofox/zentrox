package z_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aminofox/zentrox/v2"
	"github.com/aminofox/zentrox/v2/middleware"
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

func TestErrorHandler_WrapError(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.ErrorHandler(middleware.DefaultErrorHandler()))
	app.GET("/err", zentrox.WrapError(func(c *zentrox.Context) error {
		return zentrox.NewHTTPError(http.StatusTeapot, "short and stout")
	}))

	req := httptest.NewRequest(http.MethodGet, "/err", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusTeapot {
		t.Fatalf("want 418, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "short and stout") {
		t.Fatalf("expected public error message, got %q", w.Body.String())
	}
}

func TestErrorHandler_UnknownErrorDoesNotLeakDetail(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.ErrorHandler(middleware.DefaultErrorHandler()))
	app.GET("/err", zentrox.WrapError(func(c *zentrox.Context) error {
		return errors.New("database password leaked in internal detail")
	}))

	req := httptest.NewRequest(http.MethodGet, "/err", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "database password") {
		t.Fatalf("unknown error detail leaked to client: %q", w.Body.String())
	}
}

func TestErrorHandler_DoesNotDoubleWriteCommittedResponse(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.ErrorHandler(middleware.DefaultErrorHandler()))
	app.GET("/partial", func(c *zentrox.Context) {
		if err := c.String(http.StatusAccepted, "already written"); err != nil {
			t.Fatal(err)
		}
		c.SetError(errors.New("late failure"))
	})

	req := httptest.NewRequest(http.MethodGet, "/partial", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want original 202, got %d", w.Code)
	}
	if got := w.Body.String(); got != "already written" {
		t.Fatalf("body should not be overwritten, got %q", got)
	}
}
