package zentrox

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Handler is the middleware/handler function type.
type Handler func(*Context)

type RouteInfo struct {
	Method      string
	Path        string
	HandlerName string
	Middlewares []string
	File        string
	Line        int
}

// App is the main entrypoint of the framework.
type App struct {
	rt   *router
	plug []Handler // global middlewares

	// Optional lifecycle hooks.
	// onRequest: called just after Context is initialized (before middleware chain).
	// onResponse: called after chain finishes (status might be 0 -> treat as 200).
	onRequest  func(*Context)
	onResponse func(*Context, int, time.Duration)

	// onPanic is invoked when a panic happens inside the chain.
	// IMPORTANT: we re-throw the panic so existing Recovery/ErrorHandler can handle it.
	onPanic func(*Context, any)

	// NotFound is an optional hook to render 404 responses.
	// If nil, the default http.NotFound is used.
	notFound Handler

	// Optional application version string; propagated to context as "app_version".
	version string

	// enable openapi
	enableOpenapi bool

	// enable route printing when Run()
	printRoutes bool
	// registry all registered routes
	routeIndex map[string]RouteInfo
}

// ServerConfig controls the underlying http.Server configuration.
// All fields are optional; sensible defaults are applied.
type ServerConfig struct {
	// Address to listen on, e.g. ":8000".
	Addr string

	// Timeouts protect the server from slow or stuck clients.
	// Defaults: ReadHeader=5s, Read=15s, Write=30s, Idle=60s.
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration

	// Upper bound for request headers (default 1 MiB).
	MaxHeaderBytes int

	// Where to write internal http.Server logs.
	// Default: stderr with prefix "zentrox/http: ".
	ErrorLog *log.Logger

	// BaseContext sets the base context for all connections (optional).
	BaseContext func(net.Listener) context.Context
}

func NewApp() *App {
	return &App{
		rt:         newRouter(),
		routeIndex: make(map[string]RouteInfo),
	}
}

// Plug registers global middlewares in declared order.
func (a *App) Plug(m ...Handler) {
	a.plug = append(a.plug, m...)
}

// On registers a route with a custom HTTP method.
func (a *App) on(method, path string, hs ...Handler) {
	if len(hs) == 0 {
		panic("zentrox: On requires at least one handler")
	}
	h := hs[len(hs)-1]    // main handler: last element
	mws := hs[:len(hs)-1] // route middlewares
	a.rt.add(method, path, append(a.plug, mws...), h)
	a.trackRoute(method, path, h, append(a.plug, mws...))

	// Auto-register OPTIONS handler if not already registered
	if method != http.MethodOptions {
		optHandler := func(c *Context) {
			// Use SendStatus to ensure proper status code recording
			c.SendStatus(http.StatusNoContent)
		}
		a.rt.add(http.MethodOptions, path, append(a.plug, mws...), optHandler)
	}
}

// Sugar helpers.
func (a *App) OnGet(path string, handlers ...Handler) {
	a.on(http.MethodGet, path, handlers...)
}

func (a *App) OnPost(path string, handlers ...Handler) {
	a.on(http.MethodPost, path, handlers...)
}

func (a *App) OnPut(path string, handlers ...Handler) {
	a.on(http.MethodPut, path, handlers...)
}

func (a *App) OnPatch(path string, handlers ...Handler) {
	a.on(http.MethodPatch, path, handlers...)
}

func (a *App) OnDelete(path string, handlers ...Handler) {
	a.on(http.MethodDelete, path, handlers...)
}

// Scope creates a route group with a path prefix and optional middlewares.
func (a *App) Scope(prefix string, mws ...Handler) *Scope {
	return &Scope{app: a, prefix: prefix, plug: append([]Handler{}, mws...)}
}

// ServeHTTP uses a context pool and the precompiled router to handle the request.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Acquire a pooled Context instance.
	ctx := acquireContext(w, r)
	defer releaseContext(ctx)

	// Wrap writer to capture status/bytes for onResponse.
	rr := &respRecorder{ResponseWriter: w}
	// Lifecycle: onRequest
	if a.onRequest != nil {
		a.onRequest(ctx)
	}

	// Propagate app version to context for logs/metrics.
	if a.version != "" {
		ctx.Set(AppVersion, a.version)
	}

	// Start timer for latency and ensure onResponse fires for all branches.
	start := time.Now()
	defer func() {
		if a.onResponse != nil {
			st := rr.status
			if st == 0 {
				st = http.StatusOK
			}
			a.onResponse(ctx, st, time.Since(start))
		}
	}()

	// Panic hook: notify then rethrow so Recovery/ErrorHandler can handle it.
	defer func() {
		if rec := recover(); rec != nil {
			if a.onPanic != nil {
				a.onPanic(ctx, rec)
			}
			panic(rec)
		}
	}()

	// Try exact method match first.
	entry := a.rt.match(r.Method, r.URL.Path, ctx.params)

	// Automatic HEAD: if HEAD is not registered, reuse GET handler without body.
	if entry == nil && r.Method == http.MethodHead {
		if getEntry := a.rt.match(http.MethodGet, r.URL.Path, ctx.params); getEntry != nil {
			hw := &headWriter{ResponseWriter: rr} // layer over rr so status is captured
			ctx.Writer = hw
			ctx.stack = getEntry.stack
			ctx.Forward()
			return
		}
	}

	if entry == nil {
		// Compute allowed methods for this path.
		allow := a.rt.allowed(r.URL.Path)
		if len(allow) > 0 {
			rr.Header().Set("Allow", strings.Join(allow, ", "))

			// Basic OPTIONS handling: advertise allowed methods (204).
			if r.Method == http.MethodOptions {
				rr.WriteHeader(http.StatusNoContent)
				return
			}

			// 405 Method Not Allowed when path exists but method is not registered.
			http.Error(rr, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		// 404 Not Found (custom hook if provided).
		if a.notFound != nil {
			ctx.stack = []Handler{a.notFound}
			ctx.Forward()
			return
		}
		http.NotFound(rr, r)
		return
	}

	// Assign the compiled stack, then run chain.
	ctx.stack = entry.stack
	ctx.Forward()
}

// Run keeps backward compatibility: starts a blocking server with
// production-leaning defaults. Equivalent to ListenAndServe.
func (a *App) Run(addr string) error {
	cfg := &ServerConfig{Addr: addr}
	srv := a.buildServer(cfg)
	return srv.ListenAndServe()
}

// buildServer constructs an *http.Server with defaults applied.
func (a *App) buildServer(cfg *ServerConfig) *http.Server {
	// Defaults chosen for production-leaning safety.
	c := ServerConfig{
		Addr:              ":8000",
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}
	if cfg != nil {
		if cfg.Addr != "" {
			c.Addr = cfg.Addr
		}
		if cfg.ReadHeaderTimeout > 0 {
			c.ReadHeaderTimeout = cfg.ReadHeaderTimeout
		}
		if cfg.ReadTimeout > 0 {
			c.ReadTimeout = cfg.ReadTimeout
		}
		if cfg.WriteTimeout > 0 {
			c.WriteTimeout = cfg.WriteTimeout
		}
		if cfg.IdleTimeout > 0 {
			c.IdleTimeout = cfg.IdleTimeout
		}
		if cfg.MaxHeaderBytes > 0 {
			c.MaxHeaderBytes = cfg.MaxHeaderBytes
		}
		if cfg.ErrorLog != nil {
			c.ErrorLog = cfg.ErrorLog
		}
		if cfg.BaseContext != nil {
			c.BaseContext = cfg.BaseContext
		}
	}
	if c.ErrorLog == nil {
		c.ErrorLog = log.New(os.Stderr, "zentrox/http: ", log.LstdFlags)
	}

	srv := &http.Server{
		Addr:              c.Addr,
		Handler:           a, // App implements http.Handler
		ReadHeaderTimeout: c.ReadHeaderTimeout,
		ReadTimeout:       c.ReadTimeout,
		WriteTimeout:      c.WriteTimeout,
		IdleTimeout:       c.IdleTimeout,
		MaxHeaderBytes:    c.MaxHeaderBytes,
		ErrorLog:          c.ErrorLog,
	}
	if c.BaseContext != nil {
		srv.BaseContext = c.BaseContext
	}
	if a.printRoutes {
		a.PrintRoutes(os.Stdout)
	}
	return srv
}

// Start starts the server in a new goroutine and returns *http.Server.
// This is recommended in production to manage lifecycle explicitly.
func (a *App) Start(cfg *ServerConfig) (*http.Server, error) {
	srv := a.buildServer(cfg)
	go func() {
		// ListenAndServe returns http.ErrServerClosed on Shutdown; do not treat as error.
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			srv.ErrorLog.Printf("listen error: %v", err)
		}
	}()
	return srv, nil
}

// StartTLS starts a TLS server in a new goroutine and returns *http.Server.
func (a *App) StartTLS(cfg *ServerConfig, certFile, keyFile string) (*http.Server, error) {
	srv := a.buildServer(cfg)
	go func() {
		if err := srv.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
			srv.ErrorLog.Printf("listen (tls) error: %v", err)
		}
	}()
	return srv, nil
}

// Shutdown requests a graceful stop. The server stops accepting new connections
// and waits for in-flight requests until ctx is done.
func (a *App) Shutdown(ctx context.Context, srv *http.Server) error {
	return srv.Shutdown(ctx)
}

// Health mounts tiny health endpoints onto the current App.
// - If livenessPath is non-empty, it returns 200 when the process is alive.
// - If readinessPath is non-empty and ready != nil, it returns 200/503 based on ready().
func (a *App) Health(livenessPath, readinessPath string, ready func() bool) {
	if livenessPath != "" {
		a.OnGet(livenessPath, func(c *Context) { c.SendText(http.StatusOK, "ok") })
	}
	if readinessPath != "" && ready != nil {
		a.OnGet(readinessPath, func(c *Context) {
			if ready() {
				c.SendText(http.StatusOK, "ready")
				return
			}
			c.SendText(http.StatusServiceUnavailable, "not ready")
		})
	}
}

// SetOnRequest registers a hook called at the start of handling a request.
func (a *App) SetOnRequest(fn func(*Context)) *App {
	a.onRequest = fn
	return a
}

// SetOnResponse registers a hook called after the request is handled.
// Parameters: (ctx, statusCode, latency).
func (a *App) SetOnResponse(fn func(*Context, int, time.Duration)) *App {
	a.onResponse = fn
	return a
}

// SetNotFound sets a custom 404 handler hook.
func (a *App) SetNotFound(h Handler) *App {
	a.notFound = h
	return a
}

// SetOnPanic registers a hook called when a panic occurs.
// The panic value is forwarded and will be re-panicked after the hook returns.
func (a *App) SetOnPanic(fn func(*Context, any)) *App {
	a.onPanic = fn
	return a
}

// SetVersion configures an application version string injected per request.
func (a *App) SetVersion(v string) *App {
	a.version = v
	return a
}

// Version returns the configured application version.
func (a *App) Version() string {
	return a.version
}

func (a *App) SetEnableOpenAPI(enable bool) *App {
	a.enableOpenapi = enable
	return a
}

func (a *App) EnableOpenAPI() bool {
	return a.enableOpenapi
}

// Enable/disable route printing when server starts
func (a *App) SetPrintRoutes(v bool) *App {
	a.printRoutes = v
	return a
}

// Get route list (copy & sort for stability)
func (a *App) ListRoutes() []RouteInfo {
	if len(a.routeIndex) == 0 {
		return nil
	}
	out := make([]RouteInfo, 0, len(a.routeIndex))
	for _, ri := range a.routeIndex {
		out = append(out, ri)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == out[j].Path {
			return out[i].Method < out[j].Method
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func (a *App) updateRouteName(method, fullPath, handlerName string) {
	if handlerName == "" {
		return
	}
	key := strings.ToUpper(method) + "\t" + fullPath
	ri, ok := a.routeIndex[key]
	if !ok {
		return
	}
	ri.HandlerName = handlerName
	a.routeIndex[key] = ri
}

func (a *App) PrintRoutes(w io.Writer) {
	for _, r := range a.ListRoutes() {
		mw := r.Middlewares
		info := r.HandlerName
		if r.File != "" && r.Line > 0 {
			info = fmt.Sprintf("%s (%s:%d)", info, path.Base(r.File), r.Line)
		}
		if len(mw) == 0 {
			fmt.Fprintf(w, " %-6s %-32s -> %s\n", "["+r.Method+"]", r.Path, info)
		} else {
			fmt.Fprintf(w, " %-6s %-32s -> %s  (mw: %s)\n",
				"["+r.Method+"]", r.Path, info, strings.Join(mw, ", "))
		}
	}
}

func handlerName(h Handler) (string, string, int) {
	if h == nil {
		return "", "", 0
	}
	p := reflect.ValueOf(h).Pointer()
	if p == 0 {
		return "", "", 0
	}
	fn := runtime.FuncForPC(p)
	if fn == nil {
		return "", "", 0
	}
	name := fn.Name()
	file, line := fn.FileLine(p)

	// shorten function name
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	// keep the part after the dot to remove the package prefix
	if i := strings.Index(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name, file, line
}

func middlewareNames(mws []Handler) []string {
	out := make([]string, 0, len(mws))
	for _, mw := range mws {
		n, _, _ := handlerName(mw)
		out = append(out, n)
	}
	return out
}

// internal helper to track each registration
func (a *App) trackRoute(method, fullPath string, h Handler, mws []Handler) {
	if a.routeIndex == nil {
		a.routeIndex = make(map[string]RouteInfo)
	}
	key := strings.ToUpper(method) + "\t" + fullPath
	hn, file, line := handlerName(h)
	a.routeIndex[key] = RouteInfo{
		Method:      strings.ToUpper(method),
		Path:        fullPath,
		HandlerName: hn,
		Middlewares: middlewareNames(mws),
		File:        file,
		Line:        line,
	}
}

// Scope (Route Group)
type Scope struct {
	app    *App
	prefix string
	plug   []Handler // group-level middlewares
}

func (s *Scope) on(method, rel string, hs ...Handler) {
	if len(hs) == 0 {
		panic("zentrox: Scope.On requires at least one handler")
	}
	h := hs[len(hs)-1]
	mws := hs[:len(hs)-1]
	stack := append(s.app.plug, append(s.plug, mws...)...)
	s.app.rt.add(method, s.prefix+rel, stack, h)
	s.app.trackRoute(method, s.prefix+rel, h, stack)
}
func (s *Scope) OnGet(path string, handlers ...Handler) {
	s.on(http.MethodGet, path, handlers...)
}

func (s *Scope) OnPost(path string, handlers ...Handler) {
	s.on(http.MethodPost, path, handlers...)
}

func (s *Scope) OnPut(path string, handlers ...Handler) {
	s.on(http.MethodPut, path, handlers...)
}

func (s *Scope) OnPatch(path string, handlers ...Handler) {
	s.on(http.MethodPatch, path, handlers...)
}

func (s *Scope) OnDelete(path string, handlers ...Handler) {
	s.on(http.MethodDelete, path, handlers...)
}

// Context pooling
var ctxPool = sync.Pool{
	New: func() any {
		return &Context{
			params: map[string]string{},
			store:  map[string]any{},
			index:  -1,
		}
	},
}

func acquireContext(w http.ResponseWriter, r *http.Request) *Context {
	c := ctxPool.Get().(*Context)
	c.Writer = w
	c.Request = r
	c.index = -1
	c.aborted = false
	c.err = nil
	// params/store already exists; release will only delete the key
	return c
}

func releaseContext(c *Context) {
	// Clean maps without reallocations.
	for k := range c.params {
		delete(c.params, k)
	}
	for k := range c.store {
		delete(c.store, k)
	}
	// Clear references to avoid retaining memory.
	c.Writer = nil
	c.Request = nil
	c.stack = nil
	c.err = nil
	c.aborted = false
	c.index = -1

	ctxPool.Put(c)
}

// headWriter suppresses response body writes while still allowing headers/status.
// It is used to implement automatic HEAD behavior by reusing GET handlers.
type headWriter struct {
	http.ResponseWriter
	wroteHeader bool
	status      int
}

func (w *headWriter) WriteHeader(code int) {
	w.status = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *headWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	// Discard body for HEAD responses; pretend it was written successfully.
	return len(b), nil
}

// respRecorder captures status code and bytes without changing behavior.
// It is used to feed onResponse hook with final status/latency.
type respRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *respRecorder) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *respRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// StaticOptions controls behavior of Static(...)
type StaticOptions struct {
	// Directory on disk to serve from (absolute or relative to process cwd).
	Dir string
	// Optional index filename to serve when requesting the prefix root (e.g. "index.html").
	Index string
	// If true, do not auto-serve index when the request equals the prefix.
	DisableIndex bool
	// If non-zero, sets "Cache-Control: public, max-age=<seconds>" (otherwise no-cache).
	MaxAge time.Duration
	// If true, use strong ETag (SHA1 of content). Otherwise weak ETag (size-modtime).
	UseStrongETag bool
	// Optional allow-list of file extensions (lowercase, with dot), e.g. []string{".css",".js",".png"}.
	AllowedExt []string
}

// Static mounts a read-only file server under a prefix.
// It sets ETag and Last-Modified, and handles If-None-Match / If-Modified-Since.
// Security notes:
// - Prevents path traversal ("..") by cleaning and validating joined path.
// - Optional extension allow-list (if non-empty).
func (a *App) Static(prefix string, opt StaticOptions) {
	if prefix == "" || prefix[0] != '/' {
		panic("Static: prefix must start with '/'")
	}
	if opt.Dir == "" {
		panic("Static: Dir is required")
	}
	// Ensure prefix has no trailing slash (except root "/")
	if len(prefix) > 1 && strings.HasSuffix(prefix, "/") {
		prefix = strings.TrimRight(prefix, "/")
	}

	root, err := filepath.Abs(opt.Dir)
	if err != nil {
		panic("Static: cannot resolve directory: " + err.Error())
	}
	// Prebuild allow-list map
	allow := map[string]struct{}{}
	for _, e := range opt.AllowedExt {
		e = strings.ToLower(strings.TrimSpace(e))
		if e != "" && e[0] == '.' {
			allow[e] = struct{}{}
		}
	}

	// Register GET and HEAD with wildcard for subpaths.
	pat := prefix + "/*filepath"
	h := func(c *Context) {
		rel := c.Param("filepath")
		// When requesting the prefix root ("/assets" == "/assets/"), serve index if allowed
		if rel == "" || rel == "/" {
			if !opt.DisableIndex && opt.Index != "" {
				rel = "/" + opt.Index
			} else {
				c.SendText(http.StatusNotFound, "not found")
				return
			}
		}

		// Clean and join; prevent traversal outside root
		clean := filepath.Clean(rel)
		if strings.HasPrefix(clean, "..") {
			c.SendText(http.StatusForbidden, "forbidden")
			return
		}
		target := filepath.Join(root, strings.TrimPrefix(clean, string(filepath.Separator)))
		if !isWithinBase(root, target) {
			c.SendText(http.StatusForbidden, "forbidden")
			return
		}

		// Extension allow-list check (if provided)
		if len(allow) > 0 {
			ext := strings.ToLower(filepath.Ext(target))
			if _, ok := allow[ext]; !ok {
				c.SendText(http.StatusForbidden, "forbidden")
				return
			}
		}

		// Stat file
		fi, err := os.Stat(target)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				c.SendText(http.StatusNotFound, "not found")
				return
			}
			c.SendText(http.StatusInternalServerError, "stat error")
			return
		}
		if fi.IsDir() {
			// If directory is requested, optionally serve index
			if !opt.DisableIndex && opt.Index != "" {
				target = filepath.Join(target, opt.Index)
				fi, err = os.Stat(target)
				if err != nil || fi.IsDir() {
					c.SendText(http.StatusNotFound, "not found")
					return
				}
			} else {
				c.SendText(http.StatusNotFound, "not found")
				return
			}
		}

		// Compute ETag
		etag, lastMod := "", fi.ModTime().UTC()
		if opt.UseStrongETag {
			if sum, err := sha1File(target); err == nil {
				etag = `"` + hex.EncodeToString(sum) + `"`
			}
		} else {
			// Weak etag from size and seconds of mtime
			etag = `W/"` + strconv.FormatInt(fi.Size(), 10) + "-" + strconv.FormatInt(lastMod.Unix(), 10) + `"`
		}
		if etag != "" {
			c.SetHeader("ETag", etag)
		}
		c.SetHeader("Last-Modified", lastMod.Format(http.TimeFormat))

		// Cache control
		if opt.MaxAge > 0 {
			sec := int(opt.MaxAge / time.Second)
			c.SetHeader("Cache-Control", "public, max-age="+strconv.Itoa(sec))
		} else {
			c.SetHeader("Cache-Control", "no-cache")
		}

		// Conditional requests
		if inm := c.GetHeader("If-None-Match"); inm != "" && etag != "" {
			if etagMatch(inm, etag) {
				c.Writer.WriteHeader(http.StatusNotModified)
				return
			}
		}
		if ims := c.GetHeader("If-Modified-Since"); ims != "" {
			if t, err := time.Parse(http.TimeFormat, ims); err == nil {
				// If not modified since, return 304
				if !lastMod.After(t) {
					c.Writer.WriteHeader(http.StatusNotModified)
					return
				}
			}
		}

		// Content-Type best effort
		if ct := mime.TypeByExtension(filepath.Ext(target)); ct != "" {
			c.SetHeader("Content-Type", ct)
		}

		// HEAD should not write body
		if c.Request.Method == http.MethodHead {
			c.Writer.WriteHeader(http.StatusOK)
			return
		}

		// Stream the file to client
		f, err := os.Open(target)
		if err != nil {
			c.SendText(http.StatusInternalServerError, "open error")
			return
		}
		defer f.Close()

		c.Writer.WriteHeader(http.StatusOK)
		_, _ = io.Copy(c.Writer, f)
	}

	a.OnGet(pat, h)
	// Reuse GET handler for HEAD (HEAD auto fallback also exists, but register explicit)
	a.on(http.MethodHead, pat, h)
}

// isWithinBase ensures child is inside base to prevent path traversal.
func isWithinBase(base, child string) bool {
	b, _ := filepath.Abs(base)
	c, _ := filepath.Abs(child)
	rel, err := filepath.Rel(b, c)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// sha1File returns the SHA1 content hash (used for strong ETag).
func sha1File(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// etagMatch checks If-None-Match header against the computed ETag.
func etagMatch(header, etag string) bool {
	// If-None-Match can contain multiple values: W/"...", "..."
	parts := strings.Split(header, ",")
	for _, p := range parts {
		if strings.TrimSpace(p) == etag {
			return true
		}
	}
	return false
}
