package zentrox

import (
	"fmt"
	"io"
	"path"
	"reflect"
	"runtime"
	"sort"
	"strings"
)

// RouteInfo holds metadata about a registered route.
type RouteInfo struct {
	Method      string
	Path        string
	HandlerName string
	Middlewares []string
	File        string
	Line        int
}

// ListRoutes returns all registered routes sorted by path and method.
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

// PrintRoutes writes all registered routes to the given writer.
func (a *App) PrintRoutes(w io.Writer) {
	for _, r := range a.ListRoutes() {
		mw := r.Middlewares
		info := r.HandlerName
		if r.File != "" && r.Line > 0 {
			info = fmt.Sprintf("%s (%s:%d)", info, path.Base(r.File), r.Line)
		}
		if len(mw) == 0 {
			_, _ = fmt.Fprintf(w, " %-6s %-32s -> %s\n", "["+r.Method+"]", r.Path, info)
		} else {
			_, _ = fmt.Fprintf(w, " %-6s %-32s -> %s  (mw: %s)\n",
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

	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
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
