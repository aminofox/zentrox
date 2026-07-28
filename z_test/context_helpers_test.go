package z_test

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/aminofox/zentrox/v2"
)

type noFlushWriter struct {
	header http.Header
}

func (w *noFlushWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *noFlushWriter) WriteHeader(int) {}

func (w *noFlushWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

type failingFlushWriter struct {
	header http.Header
}

func (w *failingFlushWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *failingFlushWriter) WriteHeader(int) {}

func (w *failingFlushWriter) Write([]byte) (int, error) {
	return 0, errors.New("write failed")
}

func (w *failingFlushWriter) Flush() {}

func TestStreamingHelpersReportUnsupportedFlush(t *testing.T) {
	c := &zentrox.Context{
		Writer:  &noFlushWriter{},
		Request: httptest.NewRequest(http.MethodGet, "/stream", nil),
	}

	if err := c.PushStream(func(io.Writer, func()) {}); !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("PushStream want http.ErrNotSupported, got %v", err)
	}
	if err := c.PushSSE(func(event func(string, string)) {
		event("message", "hello")
	}); !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("PushSSE want http.ErrNotSupported, got %v", err)
	}
}

func TestPushSSEReportsWriteError(t *testing.T) {
	c := &zentrox.Context{
		Writer:  &failingFlushWriter{},
		Request: httptest.NewRequest(http.MethodGet, "/events", nil),
	}

	err := c.PushSSE(func(event func(string, string)) {
		event("message", "hello")
	})
	if err == nil {
		t.Fatal("PushSSE should return write error")
	}
}

func TestPushSSEFormatsMultilineDataAndSanitizesEventName(t *testing.T) {
	w := httptest.NewRecorder()
	c := &zentrox.Context{
		Writer:  w,
		Request: httptest.NewRequest(http.MethodGet, "/events", nil),
	}

	err := c.PushSSE(func(event func(string, string)) {
		event("mes\nsage\r", "hello\r\nworld")
	})
	if err != nil {
		t.Fatalf("PushSSE returned error: %v", err)
	}

	want := "event: message\ndata: hello\ndata: world\n\n"
	if got := w.Body.String(); got != want {
		t.Fatalf("body want %q, got %q", want, got)
	}
	if got := w.Header().Get(zentrox.HeaderConnection); got != "" {
		t.Fatalf("SSE should not force Connection header, got %q", got)
	}
}

func TestSendAttachmentReturnsOpenError(t *testing.T) {
	w := httptest.NewRecorder()
	c := &zentrox.Context{
		Writer:  w,
		Request: httptest.NewRequest(http.MethodGet, "/download", nil),
	}

	if err := c.SendAttachment("does-not-exist.txt", ""); err == nil {
		t.Fatal("SendAttachment should return open error")
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func TestDownloadUsesFilenameAndRejectsDoubleWrite(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "source.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	w := httptest.NewRecorder()
	c := &zentrox.Context{
		Writer:  w,
		Request: httptest.NewRequest(http.MethodGet, "/download", nil),
	}

	if err := c.Download(path, "report.txt"); err != nil {
		t.Fatalf("Download returned error: %v", err)
	}
	if got := w.Header().Get(zentrox.HeaderContentDisposition); got != `attachment; filename=report.txt` {
		t.Fatalf("content-disposition unexpected: %q", got)
	}
	if err := c.String(http.StatusOK, "late"); !errors.Is(err, zentrox.ErrResponseCommitted) {
		t.Fatalf("late write want ErrResponseCommitted, got %v", err)
	}
}

func TestContextCopyKeepsRequestMetadata(t *testing.T) {
	app := zentrox.NewApp()
	app.SetTrustedProxies("10.0.0.0/8")
	app.GET("/users/:id", func(c *zentrox.Context) {
		c.Set("user", "alice")
		cp := c.Copy()

		if cp.Writer != nil {
			t.Fatal("copied context should not retain ResponseWriter")
		}
		if got := cp.Param("id"); got != "42" {
			t.Fatalf("copied param want 42, got %q", got)
		}
		if got := cp.RoutePath(); got != "/users/:id" {
			t.Fatalf("copied route want /users/:id, got %q", got)
		}
		if got := cp.RealIP(); got != "203.0.113.10" {
			t.Fatalf("copied RealIP want 203.0.113.10, got %q", got)
		}
		if got, ok := cp.Get("user"); !ok || got != "alice" {
			t.Fatalf("copied store want alice, got %v ok=%v", got, ok)
		}
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/users/42", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	req.Header.Set(zentrox.HeaderXForwardedFor, "203.0.113.10, 10.0.0.1")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
}

func TestSaveUploadedFileRefusesSymlinkOverwrite(t *testing.T) {
	tmp := t.TempDir()
	outside := filepath.Join(tmp, "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write outside: %v", err)
	}
	uploads := filepath.Join(tmp, "uploads")
	if err := os.MkdirAll(uploads, 0o755); err != nil {
		t.Fatalf("mkdir uploads: %v", err)
	}
	link := filepath.Join(uploads, "avatar.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "avatar.txt")
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write([]byte("overwrite")); err != nil {
		t.Fatalf("write multipart: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set(zentrox.HeaderContentType, mw.FormDataContentType())
	c := &zentrox.Context{Request: req, Writer: httptest.NewRecorder()}

	if _, err := c.SaveUploadedFile("file", uploads, zentrox.UploadOptions{Overwrite: true}); err == nil {
		t.Fatal("SaveUploadedFile should refuse symlink overwrite")
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "secret" {
		t.Fatalf("outside file changed: %q err=%v", string(got), err)
	}
}
