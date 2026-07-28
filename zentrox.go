package zentrox

import (
	"bufio"
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
	"net/netip"
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

	"github.com/aminofox/zentrox/v2/validation"
)

// Handler is the middleware/handler function type.
type Handler func(*Context)

// HandlerWithError is an opt-in handler shape for applications that prefer
// returning business errors and letting middleware map them centrally.
type HandlerWithError func(*Context) error

// WrapError adapts a HandlerWithError to the standard Handler type.
func WrapError(h HandlerWithError) Handler {
	return func(c *Context) {
		if err := h(c); err != nil {
			c.SetError(err)
		}
	}
}

// WrapHTTP adapts a standard net/http handler to a Zentrox handler.
func WrapHTTP(h http.Handler) Handler {
	return func(c *Context) {
		h.ServeHTTP(c.Writer, c.Request)
	}
}

// WrapHTTPFunc adapts a standard net/http handler function to a Zentrox handler.
func WrapHTTPFunc(h func(http.ResponseWriter, *http.Request)) Handler {
	return WrapHTTP(http.HandlerFunc(h))
}

// SlashBehavior controls how request paths with repeated or trailing slashes are handled.
type SlashBehavior int

const (
	// SlashNormalize keeps the historical behavior: repeated/trailing slashes are ignored by routing.
	SlashNormalize SlashBehavior = iota
	// SlashStrict rejects non-canonical slash forms with 404.
	SlashStrict
	// SlashRedirectClean redirects non-canonical slash forms to path.Clean(path).
	SlashRedirectClean
)

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
	mu     sync.RWMutex
	frozen bool

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

	// enable route printing when Run()
	printRoutes bool
	// registry all registered routes
	routeIndex map[string]RouteInfo

	trustedProxies []netip.Prefix
	trustAllProxy  bool
	slashBehavior  SlashBehavior
	validator      validation.StructValidator
	jsonCodec      JSONCodec
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
		validator:  validation.DefaultValidator(),
		jsonCodec:  DefaultJSONCodec(),
	}
}

func (a *App) freeze() {
	a.mu.RLock()
	frozen := a.frozen
	a.mu.RUnlock()
	if frozen {
		return
	}

	a.mu.Lock()
	a.frozen = true
	a.mu.Unlock()
}

func (a *App) assertMutableLocked(op string) {
	if a.frozen {
		panic("zentrox: cannot " + op + " after app has started serving")
	}
}

// Plug registers global middlewares in declared order.
func (a *App) Plug(m ...Handler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("register middleware")
	a.plug = append(a.plug, m...)
}

// SetValidator replaces the validator used by Bind*Into helpers.
// It must be called during startup before the app starts serving.
func (a *App) SetValidator(v validation.StructValidator) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("set validator")
	if v == nil {
		v = validation.DefaultValidator()
	}
	a.validator = v
	return a
}

// SetValidatorFunc replaces the validator used by Bind*Into helpers with a function.
// For go-playground/validator, use: app.SetValidatorFunc(validator.New().Struct).
func (a *App) SetValidatorFunc(fn func(any) error) *App {
	if fn == nil {
		return a.SetValidator(nil)
	}
	return a.SetValidator(validation.StructValidatorFunc(fn))
}

// SetJSONCodec replaces the JSON codec used by Context.JSON and BindJSONInto.
// It must be called during startup before the app starts serving.
func (a *App) SetJSONCodec(codec JSONCodec) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("set JSON codec")
	if codec == nil {
		codec = DefaultJSONCodec()
	}
	a.jsonCodec = codec
	return a
}

// SetJSONCodecFuncs replaces the JSON codec with marshal/unmarshal functions.
// For example: app.SetJSONCodecFuncs(json.Marshal, json.Unmarshal).
func (a *App) SetJSONCodecFuncs(marshal func(any) ([]byte, error), unmarshal func([]byte, any) error) *App {
	if marshal == nil || unmarshal == nil {
		panic("SetJSONCodecFuncs: marshal and unmarshal are required")
	}
	return a.SetJSONCodec(jsonCodecFuncs{marshal: marshal, unmarshal: unmarshal})
}

// On registers a route with a custom HTTP method.
func (a *App) on(method, path string, hs ...Handler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onLocked(method, path, hs...)
}

// Handle registers a route with a custom HTTP method.
func (a *App) Handle(method, path string, handlers ...Handler) {
	a.on(method, path, handlers...)
}

func (a *App) onLocked(method, path string, hs ...Handler) {
	a.assertMutableLocked("register routes")
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		panic("zentrox: HTTP method cannot be empty")
	}
	validateRoutePath(path)
	if len(hs) == 0 {
		panic("zentrox: On requires at least one handler")
	}
	h := hs[len(hs)-1]    // main handler: last element
	mws := hs[:len(hs)-1] // route middlewares
	a.rt.add(method, path, append(a.plug, mws...), h)
	a.trackRoute(method, path, h, append(a.plug, mws...))
}

// GET registers a route for GET requests
func (a *App) GET(path string, handlers ...Handler) {
	a.on(http.MethodGet, path, handlers...)
}

// POST registers a route for POST requests
func (a *App) POST(path string, handlers ...Handler) {
	a.on(http.MethodPost, path, handlers...)
}

// PUT registers a route for PUT requests
func (a *App) PUT(path string, handlers ...Handler) {
	a.on(http.MethodPut, path, handlers...)
}

// PATCH registers a route for PATCH requests
func (a *App) PATCH(path string, handlers ...Handler) {
	a.on(http.MethodPatch, path, handlers...)
}

// DELETE registers a route for DELETE requests
func (a *App) DELETE(path string, handlers ...Handler) {
	a.on(http.MethodDelete, path, handlers...)
}

// OPTIONS registers a route for OPTIONS requests.
func (a *App) OPTIONS(path string, handlers ...Handler) {
	a.on(http.MethodOptions, path, handlers...)
}

// Scope creates a route group with a path prefix and optional middlewares.
func (a *App) Scope(prefix string, mws ...Handler) *Scope {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("create scope")
	prefix = normalizeScopePrefix(prefix)
	return &Scope{app: a, prefix: prefix, plug: append([]Handler{}, mws...)}
}

func validateRoutePath(p string) {
	if p == "" || p[0] != '/' {
		panic("zentrox: route path must start with '/'")
	}
}

func normalizeScopePrefix(prefix string) string {
	validateRoutePath(prefix)
	if len(prefix) > 1 {
		prefix = strings.TrimRight(prefix, "/")
	}
	return prefix
}

func joinRoutePath(prefix, rel string) string {
	if rel == "" {
		rel = "/"
	}
	if rel[0] != '/' {
		rel = "/" + rel
	}
	if prefix == "" || prefix == "/" {
		return rel
	}
	return prefix + rel
}

// ServeHTTP uses a context pool and the precompiled router to handle the request.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.freeze()

	// Acquire a pooled Context instance.
	ctx := acquireContext(w, r)
	defer releaseContext(ctx)
	ctx.realIP = a.clientIP
	ctx.validator = a.validator
	ctx.jsonCodec = a.jsonCodec

	// Wrap writer to capture status/bytes for onResponse.
	rr := &respRecorder{ResponseWriter: w}
	ctx.Writer = rr
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

	if a.handleSlashBehavior(rr, r) {
		return
	}

	// Try exact method match first.
	entry := a.rt.match(r.Method, r.URL.Path, ctx.params)

	if entry == nil && r.Method == http.MethodHead {
		if getEntry := a.rt.match(http.MethodGet, r.URL.Path, ctx.params); getEntry != nil {
			hw := &headWriter{ResponseWriter: rr}
			ctx.Writer = hw
			ctx.route = getEntry.pattern
			ctx.stack = getEntry.stack
			ctx.Next()
			return
		}
	}

	if entry == nil {
		allow := a.rt.allowed(r.URL.Path)
		if len(allow) > 0 {
			rr.Header().Set(HeaderAllow, strings.Join(allow, ", "))

			if r.Method == http.MethodOptions {
				ctx.stack = append(append([]Handler{}, a.plug...), func(c *Context) {
					c.SendStatus(http.StatusNoContent)
				})
				ctx.Next()
				return
			}

			http.Error(rr, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		if a.notFound != nil {
			ctx.stack = []Handler{a.notFound}
			ctx.Next()
			return
		}
		http.NotFound(rr, r)
		return
	}

	ctx.stack = entry.stack
	ctx.route = entry.pattern
	ctx.Next()
}

func (a *App) handleSlashBehavior(w http.ResponseWriter, r *http.Request) bool {
	switch a.slashBehavior {
	case SlashStrict:
		if isNonCanonicalSlashPath(r.URL.Path) {
			http.NotFound(w, r)
			return true
		}
	case SlashRedirectClean:
		cleaned := cleanSlashPath(r.URL.Path)
		if cleaned != r.URL.Path {
			u := *r.URL
			u.Path = cleaned
			u.RawPath = ""
			http.Redirect(w, r, u.RequestURI(), http.StatusPermanentRedirect)
			return true
		}
	}
	return false
}

func isNonCanonicalSlashPath(p string) bool {
	return p == "" || strings.Contains(p, "//") || (len(p) > 1 && strings.HasSuffix(p, "/"))
}

func cleanSlashPath(p string) string {
	if p == "" {
		return "/"
	}
	cleaned := path.Clean(p)
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	if len(cleaned) > 1 && (cleaned[1] == '/' || cleaned[1] == '\\') {
		return "/"
	}
	return cleaned
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
	a.freeze()

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
		a.GET(livenessPath, func(c *Context) { c.String(http.StatusOK, "ok") })
	}
	if readinessPath != "" && ready != nil {
		a.GET(readinessPath, func(c *Context) {
			if ready() {
				c.String(http.StatusOK, "ready")
				return
			}
			c.String(http.StatusServiceUnavailable, "not ready")
		})
	}
}

// SetOnRequest registers a hook called at the start of handling a request.
func (a *App) SetOnRequest(fn func(*Context)) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("configure hooks")
	a.onRequest = fn
	return a
}

// SetOnResponse registers a hook called after the request is handled.
// Parameters: (ctx, statusCode, latency).
func (a *App) SetOnResponse(fn func(*Context, int, time.Duration)) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("configure hooks")
	a.onResponse = fn
	return a
}

// SetNotFound sets a custom 404 handler hook.
func (a *App) SetNotFound(h Handler) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("configure not found handler")
	a.notFound = h
	return a
}

// SetOnPanic registers a hook called when a panic occurs.
// The panic value is forwarded and will be re-panicked after the hook returns.
func (a *App) SetOnPanic(fn func(*Context, any)) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("configure hooks")
	a.onPanic = fn
	return a
}

// SetVersion configures an application version string injected per request.
func (a *App) SetVersion(v string) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("configure version")
	a.version = v
	return a
}

// Version returns the configured application version.
func (a *App) Version() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.version
}

// Enable/disable route printing when server starts
func (a *App) SetPrintRoutes(v bool) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("configure route printing")
	a.printRoutes = v
	return a
}

// SetTrustedProxies configures proxy ranges allowed to provide client IP headers.
// By default, no proxy is trusted and RealIP() falls back to RemoteAddr.
// Accepts CIDR blocks (e.g. "10.0.0.0/8") and single IPs (e.g. "127.0.0.1").
// Use "*" to trust all proxies.
func (a *App) SetTrustedProxies(values ...string) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("configure trusted proxies")
	a.trustedProxies = nil
	a.trustAllProxy = false

	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if raw == "*" {
			a.trustAllProxy = true
			continue
		}

		if !strings.Contains(raw, "/") {
			ip, err := netip.ParseAddr(raw)
			if err != nil {
				panic("SetTrustedProxies: invalid ip " + raw)
			}
			bits := 32
			if ip.Is6() {
				bits = 128
			}
			a.trustedProxies = append(a.trustedProxies, netip.PrefixFrom(ip, bits))
			continue
		}

		p, err := netip.ParsePrefix(raw)
		if err != nil {
			panic("SetTrustedProxies: invalid cidr " + raw)
		}
		a.trustedProxies = append(a.trustedProxies, p.Masked())
	}

	return a
}

// SetSlashBehavior configures repeated/trailing slash handling.
// Configure this before the app starts serving.
func (a *App) SetSlashBehavior(v SlashBehavior) *App {
	if v < SlashNormalize || v > SlashRedirectClean {
		panic("SetSlashBehavior: invalid slash behavior")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("configure slash behavior")
	a.slashBehavior = v
	return a
}

func (a *App) isTrustedProxy(ip netip.Addr) bool {
	if !ip.IsValid() {
		return false
	}
	if a.trustAllProxy {
		return true
	}
	for _, p := range a.trustedProxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

func splitHostIP(remoteAddr string) netip.Addr {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return netip.Addr{}
	}
	return ip
}

func parseHeaderIPs(v string) []netip.Addr {
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]netip.Addr, 0, len(parts))
	for _, p := range parts {
		ip, err := netip.ParseAddr(strings.TrimSpace(p))
		if err == nil {
			out = append(out, ip)
		}
	}
	return out
}

func (a *App) clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	remote := splitHostIP(r.RemoteAddr)
	if !remote.IsValid() {
		return ""
	}

	if !a.isTrustedProxy(remote) {
		return remote.String()
	}

	xff := parseHeaderIPs(r.Header.Get(HeaderXForwardedFor))
	if len(xff) > 0 {
		chain := append(xff, remote)
		for i := len(chain) - 1; i >= 0; i-- {
			if !a.isTrustedProxy(chain[i]) {
				return chain[i].String()
			}
		}
		return chain[0].String()
	}

	if xr := strings.TrimSpace(r.Header.Get(HeaderXRealIP)); xr != "" {
		if ip, err := netip.ParseAddr(xr); err == nil {
			return ip.String()
		}
	}

	return remote.String()
}

// Get route list (copy & sort for stability)
func (a *App) ListRoutes() []RouteInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()
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
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("update route metadata")
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
	s.app.mu.Lock()
	defer s.app.mu.Unlock()
	s.app.assertMutableLocked("register routes")
	if len(hs) == 0 {
		panic("zentrox: Scope.On requires at least one handler")
	}
	fullPath := joinRoutePath(s.prefix, rel)
	h := hs[len(hs)-1]
	mws := hs[:len(hs)-1]
	stack := append(s.app.plug, append(s.plug, mws...)...)
	s.app.rt.add(method, fullPath, stack, h)
	s.app.trackRoute(method, fullPath, h, stack)
}

// Handle registers a route with a custom HTTP method within this scope.
func (s *Scope) Handle(method, path string, handlers ...Handler) {
	s.on(method, path, handlers...)
}

// GET registers a route for GET requests
func (s *Scope) GET(path string, handlers ...Handler) {
	s.on(http.MethodGet, path, handlers...)
}

// POST registers a route for POST requests
func (s *Scope) POST(path string, handlers ...Handler) {
	s.on(http.MethodPost, path, handlers...)
}

// PUT registers a route for PUT requests
func (s *Scope) PUT(path string, handlers ...Handler) {
	s.on(http.MethodPut, path, handlers...)
}

// PATCH registers a route for PATCH requests
func (s *Scope) PATCH(path string, handlers ...Handler) {
	s.on(http.MethodPatch, path, handlers...)
}

// DELETE registers a route for DELETE requests
func (s *Scope) DELETE(path string, handlers ...Handler) {
	s.on(http.MethodDelete, path, handlers...)
}

// OPTIONS registers a route for OPTIONS requests.
func (s *Scope) OPTIONS(path string, handlers ...Handler) {
	s.on(http.MethodOptions, path, handlers...)
}

// Use adds middleware to this scope
func (s *Scope) Use(middlewares ...Handler) {
	s.app.mu.Lock()
	defer s.app.mu.Unlock()
	s.app.assertMutableLocked("register scope middleware")
	s.plug = append(s.plug, middlewares...)
}

// Scope creates a nested route group with a path prefix and optional middlewares.
func (s *Scope) Scope(prefix string, mws ...Handler) *Scope {
	s.app.mu.Lock()
	defer s.app.mu.Unlock()
	s.app.assertMutableLocked("create scope")
	prefix = normalizeScopePrefix(prefix)
	combinedMws := append([]Handler{}, s.plug...)
	combinedMws = append(combinedMws, mws...)
	return &Scope{
		app:    s.app,
		prefix: joinRoutePath(s.prefix, prefix),
		plug:   combinedMws,
	}
}

// Context pooling
var ctxPool = sync.Pool{
	New: func() any {
		return &Context{
			params: map[string]string{},
			store:  make(map[any]any),
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
	c.realIP = nil
	c.route = ""
	c.responseCommitted = false
	c.validator = nil
	c.jsonCodec = nil
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
	c.realIP = nil
	c.route = ""
	c.responseCommitted = false
	c.validator = nil
	c.jsonCodec = nil

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
	if w.wroteHeader {
		return
	}
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

func (w *headWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *headWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijacker not supported")
	}
	return h.Hijack()
}

func (w *headWriter) Push(target string, opts *http.PushOptions) error {
	p, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return p.Push(target, opts)
}

func (w *headWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *headWriter) ReadFrom(r io.Reader) (n int64, err error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return io.Copy(io.Discard, r)
}

// respRecorder captures status code and bytes without changing behavior.
// It is used to feed onResponse hook with final status/latency.
type respRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *respRecorder) Status() int {
	return w.status
}

func (w *respRecorder) BytesWritten() int {
	return w.bytes
}

func (w *respRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *respRecorder) WriteHeader(code int) {
	if w.status != 0 {
		return
	}
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

func (w *respRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *respRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijacker not supported")
	}
	return h.Hijack()
}

func (w *respRecorder) Push(target string, opts *http.PushOptions) error {
	p, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return p.Push(target, opts)
}

func (w *respRecorder) ReadFrom(r io.Reader) (n int64, err error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	rf, ok := w.ResponseWriter.(io.ReaderFrom)
	if ok {
		n, err = rf.ReadFrom(r)
	} else {
		n, err = io.Copy(w.ResponseWriter, r)
	}
	w.bytes += int(n)
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
	// If true, allow symlinks under Dir. By default symlink targets must resolve inside Dir.
	FollowSymlinks bool
}

// Static mounts a read-only file server under a prefix.
// It sets ETag and Last-Modified, and handles If-None-Match / If-Modified-Since.
// Security notes:
// - Prevents path traversal ("..") by cleaning and validating joined path.
// - Blocks symlink escapes outside Dir unless FollowSymlinks is true.
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
	rootPath := prefix
	h := func(c *Context) {
		rel := c.Param("filepath")
		// When requesting the prefix root ("/assets" == "/assets/"), serve index if allowed
		if rel == "" || rel == "/" {
			if !opt.DisableIndex && opt.Index != "" {
				rel = "/" + opt.Index
			} else {
				c.String(http.StatusNotFound, MsgNotFound)
				return
			}
		}

		// Clean and join; prevent traversal outside root
		clean := filepath.Clean(rel)
		if strings.HasPrefix(clean, "..") {
			c.String(http.StatusForbidden, MsgForbidden)
			return
		}
		target := filepath.Join(root, strings.TrimPrefix(clean, string(filepath.Separator)))
		if !isWithinBase(root, target) {
			c.String(http.StatusForbidden, MsgForbidden)
			return
		}

		// Stat file
		fi, err := os.Stat(target)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				c.String(http.StatusNotFound, MsgNotFound)
				return
			}
			c.String(http.StatusInternalServerError, MsgStatError)
			return
		}
		if fi.IsDir() {
			// If directory is requested, optionally serve index
			if !opt.DisableIndex && opt.Index != "" {
				target = filepath.Join(target, opt.Index)
				fi, err = os.Stat(target)
				if err != nil || fi.IsDir() {
					c.String(http.StatusNotFound, MsgNotFound)
					return
				}
			} else {
				c.String(http.StatusNotFound, MsgNotFound)
				return
			}
		}

		if !opt.FollowSymlinks && !isWithinBaseResolved(root, target) {
			c.String(http.StatusForbidden, MsgForbidden)
			return
		}

		// Extension allow-list check (if provided)
		if len(allow) > 0 {
			ext := strings.ToLower(filepath.Ext(target))
			if _, ok := allow[ext]; !ok {
				c.String(http.StatusForbidden, MsgForbidden)
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
			c.SetHeader(HeaderETag, etag)
		}
		c.SetHeader(HeaderLastModified, lastMod.Format(http.TimeFormat))

		// Cache control
		if opt.MaxAge > 0 {
			sec := int(opt.MaxAge / time.Second)
			c.SetHeader(HeaderCacheControl, "public, max-age="+strconv.Itoa(sec))
		} else {
			c.SetHeader(HeaderCacheControl, CacheControlNoCache)
		}

		// Conditional requests
		if inm := c.GetHeader(HeaderIfNoneMatch); inm != "" && etag != "" {
			if etagMatch(inm, etag) {
				c.Writer.WriteHeader(http.StatusNotModified)
				return
			}
		}
		if ims := c.GetHeader(HeaderIfModifiedSince); ims != "" {
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
			c.SetHeader(HeaderContentType, ct)
		}

		// HEAD should not write body
		if c.Request.Method == http.MethodHead {
			c.Writer.WriteHeader(http.StatusOK)
			return
		}

		// Stream the file to client
		f, err := os.Open(target)
		if err != nil {
			c.String(http.StatusInternalServerError, MsgOpenError)
			return
		}
		defer f.Close()

		c.Writer.WriteHeader(http.StatusOK)
		if _, err := io.Copy(c.Writer, f); err != nil {
			c.SetError(err)
		}
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.onLocked(http.MethodGet, pat, h)
	a.onLocked(http.MethodGet, rootPath, h)
	// Reuse GET handler for HEAD (HEAD auto fallback also exists, but register explicit)
	a.onLocked(http.MethodHead, pat, h)
	a.onLocked(http.MethodHead, rootPath, h)
}

// isWithinBase ensures child is inside base to prevent path traversal.
func isWithinBase(base, child string) bool {
	b, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	c, err := filepath.Abs(child)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(b, c)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func isWithinBaseResolved(base, child string) bool {
	b, err := filepath.EvalSymlinks(base)
	if err != nil {
		return false
	}
	c, err := filepath.EvalSymlinks(child)
	if err != nil {
		return false
	}
	return isWithinBase(b, c)
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
