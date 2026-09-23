package zentrox

import (
	"net/http"
)

// Group represents a route group with a shared path prefix and middleware stack.
type Group struct {
	app         *App
	prefix      string
	middlewares []Handler
}

func (g *Group) on(method, rel string, hs ...Handler) {
	g.app.mu.Lock()
	defer g.app.mu.Unlock()
	g.app.assertMutableLocked("register routes")
	if len(hs) == 0 {
		panic("zentrox: Group requires at least one handler")
	}
	fullPath := joinRoutePath(g.prefix, rel)
	h := hs[len(hs)-1]
	mws := hs[:len(hs)-1]
	stack := append(g.app.middlewares, append(g.middlewares, mws...)...)
	g.app.rt.add(method, fullPath, stack, h)
	g.app.trackRoute(method, fullPath, h, stack)
}

// Handle registers a route with a custom HTTP method within this group.
func (g *Group) Handle(method, path string, handlers ...Handler) {
	g.on(method, path, handlers...)
}

// GET registers a route for GET requests.
func (g *Group) GET(path string, handlers ...Handler) {
	g.on(http.MethodGet, path, handlers...)
}

// POST registers a route for POST requests.
func (g *Group) POST(path string, handlers ...Handler) {
	g.on(http.MethodPost, path, handlers...)
}

// PUT registers a route for PUT requests.
func (g *Group) PUT(path string, handlers ...Handler) {
	g.on(http.MethodPut, path, handlers...)
}

// PATCH registers a route for PATCH requests.
func (g *Group) PATCH(path string, handlers ...Handler) {
	g.on(http.MethodPatch, path, handlers...)
}

// DELETE registers a route for DELETE requests.
func (g *Group) DELETE(path string, handlers ...Handler) {
	g.on(http.MethodDelete, path, handlers...)
}

// OPTIONS registers a route for OPTIONS requests.
func (g *Group) OPTIONS(path string, handlers ...Handler) {
	g.on(http.MethodOptions, path, handlers...)
}

// Use adds middlewares to this route group.
func (g *Group) Use(middlewares ...Handler) *Group {
	g.app.mu.Lock()
	defer g.app.mu.Unlock()
	g.app.assertMutableLocked("register group middleware")
	g.middlewares = append(g.middlewares, middlewares...)
	return g
}

// Group creates a nested route group with a path prefix and optional middlewares.
func (g *Group) Group(prefix string, mws ...Handler) *Group {
	g.app.mu.Lock()
	defer g.app.mu.Unlock()
	g.app.assertMutableLocked("create group")
	prefix = normalizeGroupPrefix(prefix)
	combinedMws := append([]Handler{}, g.middlewares...)
	combinedMws = append(combinedMws, mws...)
	return &Group{
		app:         g.app,
		prefix:      joinRoutePath(g.prefix, prefix),
		middlewares: combinedMws,
	}
}
