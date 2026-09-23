package zentrox

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path"
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

// App is the main entrypoint of the framework.
type App struct {
	mu     sync.RWMutex
	frozen bool

	rt   *router
	plug []Handler // global middlewares

	// Optional lifecycle hooks.
	onRequest  func(*Context)
	onResponse func(*Context, int, time.Duration)
	onPanic    func(*Context, any)
	notFound   Handler

	version     string
	printRoutes bool
	routeIndex  map[string]RouteInfo

	trustedProxies []netip.Prefix
	trustAllProxy  bool
	slashBehavior  SlashBehavior
	validator      validation.StructValidator
	jsonCodec      JSONCodec
}

// ServerConfig controls the underlying http.Server configuration.
type ServerConfig struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
	ErrorLog          *log.Logger
	BaseContext       func(net.Listener) context.Context
}

// NewApp initializes a new Zentrox application.
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
func (a *App) SetValidatorFunc(fn func(any) error) *App {
	if fn == nil {
		return a.SetValidator(nil)
	}
	return a.SetValidator(validation.StructValidatorFunc(fn))
}

// SetJSONCodec replaces the JSON codec used by Context.JSON and BindJSONInto.
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
func (a *App) SetJSONCodecFuncs(marshal func(any) ([]byte, error), unmarshal func([]byte, any) error) *App {
	if marshal == nil || unmarshal == nil {
		panic("SetJSONCodecFuncs: marshal and unmarshal are required")
	}
	return a.SetJSONCodec(jsonCodecFuncs{marshal: marshal, unmarshal: unmarshal})
}

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
	h := hs[len(hs)-1]
	mws := hs[:len(hs)-1]
	a.rt.add(method, path, append(a.plug, mws...), h)
	a.trackRoute(method, path, h, append(a.plug, mws...))
}

// GET registers a route for GET requests.
func (a *App) GET(path string, handlers ...Handler) {
	a.on(http.MethodGet, path, handlers...)
}

// POST registers a route for POST requests.
func (a *App) POST(path string, handlers ...Handler) {
	a.on(http.MethodPost, path, handlers...)
}

// PUT registers a route for PUT requests.
func (a *App) PUT(path string, handlers ...Handler) {
	a.on(http.MethodPut, path, handlers...)
}

// PATCH registers a route for PATCH requests.
func (a *App) PATCH(path string, handlers ...Handler) {
	a.on(http.MethodPatch, path, handlers...)
}

// DELETE registers a route for DELETE requests.
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

// ServeHTTP handles an HTTP request with context pooling and trie routing.
func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.freeze()

	ctx := acquireContext(w, r)
	defer releaseContext(ctx)
	ctx.realIP = a.clientIP
	ctx.validator = a.validator
	ctx.jsonCodec = a.jsonCodec

	rr := &respRecorder{ResponseWriter: w}
	ctx.Writer = rr

	if a.onRequest != nil {
		a.onRequest(ctx)
	}

	if a.version != "" {
		ctx.Set(AppVersion, a.version)
	}

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
					_ = c.SendStatus(http.StatusNoContent)
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

// Run starts a blocking server on addr.
func (a *App) Run(addr string) error {
	cfg := &ServerConfig{Addr: addr}
	srv := a.buildServer(cfg)
	return srv.ListenAndServe()
}

func (a *App) buildServer(cfg *ServerConfig) *http.Server {
	a.freeze()

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
		Handler:           a,
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

// Start starts the server asynchronously and returns *http.Server.
func (a *App) Start(cfg *ServerConfig) (*http.Server, error) {
	srv := a.buildServer(cfg)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			srv.ErrorLog.Printf("listen error: %v", err)
		}
	}()
	return srv, nil
}

// StartTLS starts a TLS server asynchronously and returns *http.Server.
func (a *App) StartTLS(cfg *ServerConfig, certFile, keyFile string) (*http.Server, error) {
	srv := a.buildServer(cfg)
	go func() {
		if err := srv.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
			srv.ErrorLog.Printf("listen (tls) error: %v", err)
		}
	}()
	return srv, nil
}

// Shutdown requests graceful server shutdown.
func (a *App) Shutdown(ctx context.Context, srv *http.Server) error {
	return srv.Shutdown(ctx)
}

// Health mounts standard liveness and readiness endpoints.
func (a *App) Health(livenessPath, readinessPath string, ready func() bool) {
	if livenessPath != "" {
		a.GET(livenessPath, func(c *Context) { _ = c.String(http.StatusOK, "ok") })
	}
	if readinessPath != "" && ready != nil {
		a.GET(readinessPath, func(c *Context) {
			if ready() {
				_ = c.String(http.StatusOK, "ready")
				return
			}
			_ = c.String(http.StatusServiceUnavailable, "not ready")
		})
	}
}

// SetOnRequest registers a hook called at request start.
func (a *App) SetOnRequest(fn func(*Context)) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("configure hooks")
	a.onRequest = fn
	return a
}

// SetOnResponse registers a hook called after response completion.
func (a *App) SetOnResponse(fn func(*Context, int, time.Duration)) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("configure hooks")
	a.onResponse = fn
	return a
}

// SetNotFound sets a custom 404 handler.
func (a *App) SetNotFound(h Handler) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("configure not found handler")
	a.notFound = h
	return a
}

// SetOnPanic registers a hook called when a panic occurs.
func (a *App) SetOnPanic(fn func(*Context, any)) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("configure hooks")
	a.onPanic = fn
	return a
}

// SetVersion configures an application version string.
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

// SetPrintRoutes enables or disables route printing at startup.
func (a *App) SetPrintRoutes(v bool) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("configure route printing")
	a.printRoutes = v
	return a
}

// SetTrustedProxies configures proxy CIDRs or single IPs.
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
	return c
}

func releaseContext(c *Context) {
	for k := range c.params {
		delete(c.params, k)
	}
	for k := range c.store {
		delete(c.store, k)
	}
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
