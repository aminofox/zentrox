package z_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
)

func TestGzip_CompressesBigResponse(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.Gzip())

	big := strings.Repeat("abcdef0123456789", 1024) // 16KB
	app.GET("/big", func(c *zentrox.Context) {
		c.SetHeader("Content-Type", "text/plain")
		c.String(http.StatusOK, "%s", big)
	})

	req := httptest.NewRequest(http.MethodGet, "/big", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	app.ServeHTTP(w, req)

	if enc := w.Header().Get("Content-Encoding"); enc != "gzip" {
		t.Fatalf("expected gzip encoding, got %q", enc)
	}
	if vary := w.Header().Get("Vary"); !strings.Contains(strings.ToLower(vary), "accept-encoding") {
		t.Fatalf("expected Vary: Accept-Encoding, got %q", vary)
	}

	// Body should be gzipped and smaller than original
	zr, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip reader err: %v", err)
	}
	defer zr.Close()
	raw, _ := io.ReadAll(zr)
	if string(raw) != big {
		t.Fatalf("gunzip mismatch")
	}
}

func TestGzip_SkipSmallAndSkipTypes(t *testing.T) {
	app := zentrox.NewApp()
	app.Plug(middleware.Gzip())

	// Small body (<MinSize default 512) should not be compressed
	app.GET("/small", func(c *zentrox.Context) {
		c.SetHeader("Content-Type", "text/plain")
		c.String(http.StatusOK, "tiny")
	})

	// image content-type should be skipped even if large
	blob := strings.Repeat("x", 4096)
	app.GET("/image", func(c *zentrox.Context) {
		c.Data(http.StatusOK, "image/png", []byte(blob))
	})

	for _, path := range []string{"/small", "/image"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Accept-Encoding", "gzip")
		w := httptest.NewRecorder()
		app.ServeHTTP(w, req)
		if enc := w.Header().Get("Content-Encoding"); enc != "" {
			t.Fatalf("%s: expected no gzip, got %q", path, enc)
		}
	}
}
