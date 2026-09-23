package zentrox

import (
	"net/http"
)

// Scope represents a route group with a shared path prefix and middleware stack.
type Scope struct {
	app    *App
	prefix string
	plug   []Handler
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

// GET registers a route for GET requests.
func (s *Scope) GET(path string, handlers ...Handler) {
	s.on(http.MethodGet, path, handlers...)
}

// POST registers a route for POST requests.
func (s *Scope) POST(path string, handlers ...Handler) {
	s.on(http.MethodPost, path, handlers...)
}

// PUT registers a route for PUT requests.
func (s *Scope) PUT(path string, handlers ...Handler) {
	s.on(http.MethodPut, path, handlers...)
}

// PATCH registers a route for PATCH requests.
func (s *Scope) PATCH(path string, handlers ...Handler) {
	s.on(http.MethodPatch, path, handlers...)
}

// DELETE registers a route for DELETE requests.
func (s *Scope) DELETE(path string, handlers ...Handler) {
	s.on(http.MethodDelete, path, handlers...)
}

// OPTIONS registers a route for OPTIONS requests.
func (s *Scope) OPTIONS(path string, handlers ...Handler) {
	s.on(http.MethodOptions, path, handlers...)
}

// Use adds middleware to this scope.
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
