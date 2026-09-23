package zentrox

import (
	"net/http"
	"net/netip"
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

	rt          *router
	middlewares []Handler // global middlewares

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

// Use registers global middlewares in declared order.
func (a *App) Use(m ...Handler) *App {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("register middleware")
	a.middlewares = append(a.middlewares, m...)
	return a
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
	a.rt.add(method, path, append(a.middlewares, mws...), h)
	a.trackRoute(method, path, h, append(a.middlewares, mws...))
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

// Group creates a route group with a path prefix and optional middlewares.
func (a *App) Group(prefix string, mws ...Handler) *Group {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.assertMutableLocked("create group")
	prefix = normalizeGroupPrefix(prefix)
	return &Group{app: a, prefix: prefix, middlewares: append([]Handler{}, mws...)}
}

func validateRoutePath(p string) {
	if p == "" || p[0] != '/' {
		panic("zentrox: route path must start with '/'")
	}
}

func normalizeGroupPrefix(prefix string) string {
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
				ctx.stack = append(append([]Handler{}, a.middlewares...), func(c *Context) {
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
