package zentrox

import (
	"net/http"
	"path"
	"strings"
	"time"
)

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
