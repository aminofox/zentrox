package z_test

import (
	"bytes"
	"errors"
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

func TestAppFreezesAfterServe(t *testing.T) {
	app := zentrox.NewApp()
	app.GET("/ok", func(c *zentrox.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("registering a route after ServeHTTP should panic")
		}
	}()
	app.GET("/late", func(c *zentrox.Context) {})
}

func TestResponseHelpersRejectDoubleWrite(t *testing.T) {
	app := zentrox.NewApp()
	app.GET("/double", func(c *zentrox.Context) {
		if err := c.String(http.StatusCreated, "first"); err != nil {
			t.Fatalf("first write returned error: %v", err)
		}
		if err := c.JSON(http.StatusOK, map[string]string{"second": "write"}); !errors.Is(err, zentrox.ErrResponseCommitted) {
			t.Fatalf("second write want ErrResponseCommitted, got %v", err)
		}
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/double", nil))

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d", w.Code)
	}
	if got := w.Body.String(); got != "first" {
		t.Fatalf("body want %q, got %q", "first", got)
	}
}

func TestResponseHelpersRejectDoubleWriteWithoutAppRecorder(t *testing.T) {
	w := httptest.NewRecorder()
	c := &zentrox.Context{
		Writer:  w,
		Request: httptest.NewRequest(http.MethodGet, "/manual", nil),
	}

	if err := c.String(http.StatusOK, "first"); err != nil {
		t.Fatalf("first write returned error: %v", err)
	}
	if err := c.JSON(http.StatusAccepted, map[string]string{"second": "write"}); !errors.Is(err, zentrox.ErrResponseCommitted) {
		t.Fatalf("second write want ErrResponseCommitted, got %v", err)
	}
	if got := w.Body.String(); got != "first" {
		t.Fatalf("body want %q, got %q", "first", got)
	}
}

func TestCallingNextTwiceDoesNotRepeatHandlers(t *testing.T) {
	app := zentrox.NewApp()
	calls := 0
	app.GET("/work", func(c *zentrox.Context) {
		c.Next()
		c.Next()
	}, func(c *zentrox.Context) {
		calls++
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/work", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	if calls != 1 {
		t.Fatalf("handler should run once, got %d calls", calls)
	}
}

func TestCustomValidatorIsUsedByBinding(t *testing.T) {
	app := zentrox.NewApp()
	validatorCalled := false
	app.SetValidatorFunc(func(v any) error {
		validatorCalled = true
		return errors.New("custom validation failed")
	})
	app.POST("/users", func(c *zentrox.Context) {
		var input struct {
			Name string `json:"name"`
		}
		if err := c.BindJSONInto(&input); err != nil {
			c.Fail(http.StatusBadRequest, "invalid", err.Error())
			return
		}
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodPost, "/users", bytes.NewBufferString(`{"name":"alice"}`))
	req.Header.Set(zentrox.HeaderContentType, zentrox.ContentTypeJSON)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if !validatorCalled {
		t.Fatal("custom validator was not called")
	}
	if w.Code != http.StatusBadRequest {
		t.Fatalf("custom validator error want 400, got %d", w.Code)
	}
}

func TestSetValidatorFreezesAfterServe(t *testing.T) {
	app := zentrox.NewApp()
	app.GET("/ok", func(c *zentrox.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("setting validator after ServeHTTP should panic")
		}
	}()
	app.SetValidatorFunc(func(any) error { return nil })
}

func TestCustomJSONCodecIsUsedByResponseAndBinding(t *testing.T) {
	app := zentrox.NewApp()
	marshalCalled := false
	unmarshalCalled := false
	app.SetJSONCodecFuncs(func(v any) ([]byte, error) {
		marshalCalled = true
		return []byte(`{"custom":true}` + "\n"), nil
	}, func(data []byte, v any) error {
		unmarshalCalled = true
		return zentrox.DefaultJSONCodec().Unmarshal(data, v)
	})
	app.POST("/echo", func(c *zentrox.Context) {
		var input struct {
			Name string `json:"name"`
		}
		if err := c.BindJSONInto(&input); err != nil {
			c.Fail(http.StatusBadRequest, "invalid", err.Error())
			return
		}
		c.JSON(http.StatusCreated, input)
	})

	req := httptest.NewRequest(http.MethodPost, "/echo", bytes.NewBufferString(`{"name":"alice"}`))
	req.Header.Set(zentrox.HeaderContentType, zentrox.ContentTypeJSON)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if !marshalCalled {
		t.Fatal("custom JSON marshal was not called")
	}
	if !unmarshalCalled {
		t.Fatal("custom JSON unmarshal was not called")
	}
	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d", w.Code)
	}
	if got := w.Body.String(); got != `{"custom":true}`+"\n" {
		t.Fatalf("custom response body mismatch: %q", got)
	}
}

func TestJSONMarshalErrorDoesNotCommitResponse(t *testing.T) {
	app := zentrox.NewApp()
	app.GET("/bad", func(c *zentrox.Context) {
		if err := c.JSON(http.StatusOK, make(chan int)); err == nil {
			t.Fatal("JSON should return marshal error")
		}
		if c.ResponseCommitted() {
			t.Fatal("marshal error should not commit response")
		}
		c.String(http.StatusInternalServerError, "fallback")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/bad", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("want 500 fallback, got %d", w.Code)
	}
	if got := w.Body.String(); got != "fallback" {
		t.Fatalf("body want fallback, got %q", got)
	}
}

func TestSetJSONCodecFreezesAfterServe(t *testing.T) {
	app := zentrox.NewApp()
	app.GET("/ok", func(c *zentrox.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("setting JSON codec after ServeHTTP should panic")
		}
	}()
	app.SetJSONCodec(nil)
}

func TestHandleAndWrapHTTP(t *testing.T) {
	app := zentrox.NewApp()
	app.Handle("report", "/report", zentrox.WrapHTTP(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(r.Method))
	})))

	req := httptest.NewRequest("REPORT", "/report", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("want 201, got %d", w.Code)
	}
	if got := w.Body.String(); got != "REPORT" {
		t.Fatalf("body want %q, got %q", "REPORT", got)
	}
}

func TestRoutePathMustStartWithSlash(t *testing.T) {
	app := zentrox.NewApp()
	defer func() {
		if recover() == nil {
			t.Fatal("route without leading slash should panic")
		}
	}()
	app.GET("missing-slash", func(c *zentrox.Context) {})
}

func TestScopeAcceptsRelativePath(t *testing.T) {
	app := zentrox.NewApp()
	api := app.Scope("/api/")
	api.GET("users", func(c *zentrox.Context) {
		c.String(http.StatusOK, "ok")
	})

	w := httptest.NewRecorder()
	app.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestSlashBehaviorStrictAndRedirect(t *testing.T) {
	strict := zentrox.NewApp()
	strict.SetSlashBehavior(zentrox.SlashStrict)
	strict.GET("/users/:id", func(c *zentrox.Context) {
		c.String(http.StatusOK, "%s", c.Param("id"))
	})

	w := httptest.NewRecorder()
	strict.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/users//42", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("strict repeated slash want 404, got %d", w.Code)
	}

	redirect := zentrox.NewApp()
	redirect.SetSlashBehavior(zentrox.SlashRedirectClean)
	redirect.GET("/users/:id", func(c *zentrox.Context) {
		c.String(http.StatusOK, "%s", c.Param("id"))
	})

	w = httptest.NewRecorder()
	redirect.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/users//42/", nil))
	if w.Code != http.StatusPermanentRedirect {
		t.Fatalf("redirect clean want 308, got %d", w.Code)
	}
	if got := w.Header().Get("Location"); got != "/users/42" {
		t.Fatalf("Location want %q, got %q", "/users/42", got)
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

func TestImplicitOptionsAllowIsDeterministicAndSkipsRouteMiddleware(t *testing.T) {
	app := zentrox.NewApp()
	routeMiddlewareCalled := false
	routeMiddleware := func(c *zentrox.Context) {
		routeMiddlewareCalled = true
		c.Next()
	}

	app.GET("/resource", routeMiddleware, func(c *zentrox.Context) {
		c.String(http.StatusOK, "get")
	})
	app.POST("/resource", routeMiddleware, func(c *zentrox.Context) {
		c.String(http.StatusOK, "post")
	})

	req := httptest.NewRequest(http.MethodOptions, "/resource", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", w.Code)
	}
	if routeMiddlewareCalled {
		t.Fatal("implicit OPTIONS should not run route-specific middleware")
	}
	if got, want := w.Header().Get(zentrox.HeaderAllow), "GET, HEAD, OPTIONS, POST"; got != want {
		t.Fatalf("Allow header want %q, got %q", want, got)
	}
}

func TestExplicitOptionsCanBeRegisteredAfterGET(t *testing.T) {
	app := zentrox.NewApp()
	app.GET("/resource", func(c *zentrox.Context) {
		c.String(http.StatusOK, "get")
	})
	app.OPTIONS("/resource", func(c *zentrox.Context) {
		c.String(http.StatusAccepted, "custom options")
	})

	req := httptest.NewRequest(http.MethodOptions, "/resource", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d", w.Code)
	}
	if got := w.Body.String(); got != "custom options" {
		t.Fatalf("body want %q, got %q", "custom options", got)
	}
}

func TestScopeExplicitOptions(t *testing.T) {
	app := zentrox.NewApp()
	api := app.Scope("/api")
	api.GET("/resource", func(c *zentrox.Context) {
		c.String(http.StatusOK, "get")
	})
	api.OPTIONS("/resource", func(c *zentrox.Context) {
		c.String(http.StatusAccepted, "scope options")
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/resource", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d", w.Code)
	}
	if got := w.Body.String(); got != "scope options" {
		t.Fatalf("body want %q, got %q", "scope options", got)
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

func TestStaticBlocksSymlinkEscapeByDefault(t *testing.T) {
	app := zentrox.NewApp()
	root := t.TempDir()
	outsideDir := t.TempDir()

	outsideFile := filepath.Join(outsideDir, "secret.txt")
	if err := os.WriteFile(outsideFile, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(outsideFile, filepath.Join(root, "secret.txt")); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	app.Static("/assets", zentrox.StaticOptions{Dir: root, AllowedExt: []string{".txt"}})

	req := httptest.NewRequest(http.MethodGet, "/assets/secret.txt", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("symlink escape want 403, got %d body=%q", w.Code, w.Body.String())
	}
}

func TestStaticAllowsDirectoryIndexWithExtensionAllowList(t *testing.T) {
	app := zentrox.NewApp()
	root := t.TempDir()
	docs := filepath.Join(root, "docs")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatalf("mkdir docs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(docs, "index.html"), []byte("<h1>docs</h1>"), 0o644); err != nil {
		t.Fatalf("write index: %v", err)
	}

	app.Static("/assets", zentrox.StaticOptions{
		Dir:        root,
		Index:      "index.html",
		AllowedExt: []string{".html"},
	})

	req := httptest.NewRequest(http.MethodGet, "/assets/docs", nil)
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("directory index want 200, got %d body=%q", w.Code, w.Body.String())
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
	trustedApp := zentrox.NewApp()
	trustedApp.SetTrustedProxies("10.0.0.0/8")
	trustedApp.GET("/ip", func(c *zentrox.Context) {
		c.String(http.StatusOK, "%s", c.RealIP())
	})
	req2 := httptest.NewRequest(http.MethodGet, "/ip", nil)
	req2.RemoteAddr = "10.0.0.1:1234"
	req2.Header.Set(zentrox.HeaderXForwardedFor, "203.0.113.10, 10.1.1.1")
	w2 := httptest.NewRecorder()
	trustedApp.ServeHTTP(w2, req2)
	if got := w2.Body.String(); got != "203.0.113.10" {
		t.Fatalf("want client ip from XFF, got %q", got)
	}
}
