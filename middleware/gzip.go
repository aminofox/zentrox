package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/aminofox/zentrox"
)

// GzipOptions defines tunables for the gzip middleware.
type GzipOptions struct {
	// MinSize is the minimum uncompressed size (in bytes) to trigger compression.
	// Small responses are faster left uncompressed.
	MinSize int

	// Level is gzip compression level (gzip.BestSpeed .. gzip.BestCompression).
	// Use gzip.DefaultCompression for a balanced default.
	Level int

	// SkipTypes: if Content-Type has any of these prefixes, skip compression.
	// Example: []string{"image/", "video/", "audio/", "application/zip", "application/gzip"}
	SkipTypes []string

	// SkipIf allows custom dynamic skipping logic. If returns true, skip.
	// It is called with the current request context after headers are available.
	SkipIf func(*zentrox.Context) bool
}

// Defaults tuned for typical APIs/HTML/JSON.
var defaultGzipOptions = GzipOptions{
	MinSize:   512,
	Level:     gzip.DefaultCompression,
	SkipTypes: []string{"image/", "video/", "audio/", "application/zip", "application/gzip"},
	SkipIf: func(c *zentrox.Context) bool {
		// Skip SSE and websocket upgrades
		ct := c.Writer.Header().Get("Content-Type")
		if strings.HasPrefix(ct, "text/event-stream") { // SSE
			return true
		}
		if strings.Contains(strings.ToLower(c.GetHeader("Connection")), "upgrade") {
			return true
		}
		return false
	},
}

// writer pool per level to reduce allocations.
var gzipPools sync.Map // map[int]*sync.Pool

func getPool(level int) *sync.Pool {
	if p, ok := gzipPools.Load(level); ok {
		return p.(*sync.Pool)
	}
	p := &sync.Pool{
		New: func() any {
			w, _ := gzip.NewWriterLevel(io.Discard, level)
			return w
		},
	}
	gzipPools.Store(level, p)
	return p
}

// Gzip is the default gzip middleware with sane defaults.
func Gzip() zentrox.Handler {
	return GzipWithOptions(defaultGzipOptions)
}

// GzipWithOptions allows configuring gzip behavior.
func GzipWithOptions(opt GzipOptions) zentrox.Handler {
	return func(c *zentrox.Context) {
		if c.Request.Method == http.MethodHead {
			c.Next()
			return
		}
		if !strings.Contains(c.GetHeader("Accept-Encoding"), "gzip") {
			c.Next()
			return
		}
		if strings.Contains(strings.ToLower(c.GetHeader("Connection")), "upgrade") {
			c.Next()
			return
		}

		rw := &gzipBufferingRW{
			ResponseWriter: c.Writer,
			ctx:            c,
			opt:            opt,
			pool:           getPool(opt.Level),
		}
		c.Writer = rw

		c.Next()

		rw.finish()
	}
}

// gzipBufferingRW buffers until either min size reached or finish() is called.
// Then it decides whether to compress and writes headers/body appropriately.
type gzipBufferingRW struct {
	http.ResponseWriter
	ctx  *zentrox.Context
	opt  GzipOptions
	pool *sync.Pool

	buf         bytes.Buffer
	decided     bool // whether we decided to gzip or not
	usingGzip   bool
	gzw         *gzip.Writer
	status      int
	wroteHeader bool
}

func (g *gzipBufferingRW) WriteHeader(code int) {
	g.status = code
	g.wroteHeader = true
	// Defer actually writing until we decide (to be able to set/remove headers properly).
}

func (g *gzipBufferingRW) maybeDecide(startGzip bool) {
	if g.decided {
		return
	}
	g.decided = true

	// If custom SkipIf or status codes imply skipping, honor that.
	if g.opt.SkipIf != nil && g.opt.SkipIf(g.ctx) {
		startGzip = false
	}
	if g.status == http.StatusNoContent || g.status == http.StatusNotModified {
		startGzip = false
	}

	// Check content-type based skipping
	ct := g.Header().Get("Content-Type")
	for _, pre := range g.opt.SkipTypes {
		if pre != "" && strings.HasPrefix(ct, pre) {
			startGzip = false
			break
		}
	}

	if startGzip {
		// Prepare gzip writer from pool
		gw := g.pool.Get().(*gzip.Writer)
		gw.Reset(g.ResponseWriter)
		g.gzw = gw
		g.usingGzip = true
		// Adjust headers
		h := g.Header()
		h.Del("Content-Length")
		h.Set("Content-Encoding", "gzip")
		h.Add("Vary", "Accept-Encoding")
		if !g.wroteHeader {
			g.status = http.StatusOK
			g.wroteHeader = true
		}
		g.ResponseWriter.WriteHeader(g.status)
		// If we already have buffered data, send it compressed.
		if g.buf.Len() > 0 {
			_, _ = g.gzw.Write(g.buf.Bytes())
			g.buf.Reset()
		}
		return
	}

	// Not using gzip: write headers/body pass-through
	if !g.wroteHeader {
		g.status = http.StatusOK
		g.wroteHeader = true
	}
	g.ResponseWriter.WriteHeader(g.status)
	if g.buf.Len() > 0 {
		_, _ = g.ResponseWriter.Write(g.buf.Bytes())
		g.buf.Reset()
	}
}

func (g *gzipBufferingRW) Write(p []byte) (int, error) {
	// Buffer until threshold; decide thereafter.
	if !g.decided {
		g.buf.Write(p)
		if g.buf.Len() >= g.opt.MinSize {
			g.maybeDecide(true)
		}
		return len(p), nil
	}
	// Already decided
	if g.usingGzip {
		return g.gzw.Write(p)
	}
	return g.ResponseWriter.Write(p)
}

func (g *gzipBufferingRW) Flush() {
	// Ensure response is started
	if !g.decided {
		g.maybeDecide(false)
	}
	if g.usingGzip {
		_ = g.gzw.Flush()
		if f, ok := g.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
		return
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// finish is called by the middleware after the downstream handlers completed.
func (g *gzipBufferingRW) finish() {
	if !g.decided {
		// If nobody wrote enough, decide not to gzip and flush buffer plain.
		g.maybeDecide(false)
	}
	if g.usingGzip && g.gzw != nil {
		_ = g.gzw.Close()
		g.pool.Put(g.gzw)
		g.gzw = nil
	}
}
