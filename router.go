package zentrox

import (
	"net/http"
	"sort"
	"strings"
)

// routeEntry carries the final, compiled handler stack for a route.
type routeEntry struct {
	stack   []Handler
	pattern string
}

// routeNode represents a node in the route trie.
type routeNode struct {
	// Static children keyed by path segment (e.g. "users", "posts").
	static map[string]*routeNode

	// Parameter child (e.g. ":id"), stores the param name without the ':'.
	param *routeNode
	pname string

	// Wildcard child (e.g. "*filepath"), stores the name without the '*'.
	wildcard *routeNode
	wname    string

	// handlers per HTTP method at this node.
	handlers map[string]*routeEntry
}

// router owns the root node of the trie.
type router struct {
	root *routeNode
}

func newRouter() *router {
	return &router{root: &routeNode{static: map[string]*routeNode{}}}
}

// add compiles the pattern into the trie and attaches the final stack.
func (r *router) add(method, pattern string, mws []Handler, h Handler) {
	segs := compilePattern(pattern)

	cur := r.root
	for i, s := range segs {
		switch {
		case s.isParam:
			if cur.param == nil {
				cur.param = &routeNode{static: map[string]*routeNode{}}
				cur.pname = s.name // already without ':'
			} else if cur.pname != s.name {
				panic("route parameter name conflict: :" + cur.pname + " vs :" + s.name)
			}
			cur = cur.param
		case s.isWildcard:
			if i != len(segs)-1 {
				panic("wildcard must be the last segment")
			}
			if cur.wildcard == nil {
				cur.wildcard = &routeNode{static: map[string]*routeNode{}}
				cur.wname = s.name // already without '*'
			} else if cur.wname != s.name {
				panic("route wildcard name conflict: *" + cur.wname + " vs *" + s.name)
			}
			cur = cur.wildcard
		default:
			if cur.static == nil {
				cur.static = map[string]*routeNode{}
			}
			next := cur.static[s.literal]
			if next == nil {
				next = &routeNode{static: map[string]*routeNode{}}
				cur.static[s.literal] = next
			}
			cur = next
		}
	}

	if cur.handlers == nil {
		cur.handlers = map[string]*routeEntry{}
	}
	if _, exists := cur.handlers[method]; exists {
		panic("duplicate route registered: " + method + " " + pattern)
	}
	stack := append([]Handler{}, mws...)
	stack = append(stack, h)
	cur.handlers[method] = &routeEntry{stack: stack, pattern: pattern}
}

// match walks the trie using a zero-allocation path iterator. It fills params.
func (r *router) match(method, path string, params map[string]string) *routeEntry {
	cur := r.root
	it := newPathIter(path)

	for {
		seg, ok := it.next()
		if !ok {
			break
		}

		// Static first
		if cur.static != nil {
			if next := cur.static[seg]; next != nil {
				cur = next
				continue
			}
		}

		// Param
		if cur.param != nil {
			params[cur.pname] = seg
			cur = cur.param
			continue
		}

		// Wildcard
		if cur.wildcard != nil {
			params[cur.wname] = it.tail(seg)
			cur = cur.wildcard
			// Wildcard is always terminal.
			break
		}

		// No match at this level
		return nil
	}

	if cur.handlers == nil {
		return nil
	}
	return cur.handlers[method]
}

// findNode walks the trie using the path only (ignores HTTP method) and
// returns the final node if it exists (wildcard is terminal).
func (r *router) findNode(path string) *routeNode {
	cur := r.root
	it := newPathIter(path)
	for {
		seg, ok := it.next()
		if !ok {
			break
		}
		// Prefer static match.
		if cur.static != nil {
			if next := cur.static[seg]; next != nil {
				cur = next
				continue
			}
		}
		// Param match allows any single segment.
		if cur.param != nil {
			cur = cur.param
			continue
		}
		// Wildcard match accepts the rest of the path.
		if cur.wildcard != nil {
			cur = cur.wildcard
			break
		}
		// No node for this path.
		return nil
	}
	return cur
}

// allowed returns a list of allowed HTTP methods for the given path.
// If a GET handler exists, HEAD is included automatically.
// OPTIONS is always included when the path exists.
func (r *router) allowed(path string) []string {
	node := r.findNode(path)
	if node == nil || node.handlers == nil {
		return nil
	}
	// Collect registered methods.
	seen := map[string]struct{}{}
	out := make([]string, 0, len(node.handlers)+2)
	for m := range node.handlers {
		// Normalize to upper-case method names commonly used.
		if m == "" {
			continue
		}
		if _, ok := seen[m]; !ok {
			seen[m] = struct{}{}
			out = append(out, m)
		}
	}
	// Add implicit HEAD if GET exists but HEAD is not explicit.
	if _, hasGET := node.handlers[http.MethodGet]; hasGET {
		if _, hasHEAD := node.handlers[http.MethodHead]; !hasHEAD {
			if _, ok := seen[http.MethodHead]; !ok {
				seen[http.MethodHead] = struct{}{}
				out = append(out, http.MethodHead)
			}
		}
	}
	// Always include OPTIONS for an existing path.
	if _, ok := seen[http.MethodOptions]; !ok {
		out = append(out, http.MethodOptions)
	}
	sort.Strings(out)
	return out
}

// pattern compilation & fast path iteration
// compiledSeg represents one segment of a compiled pattern.
type compiledSeg struct {
	literal    string // for static segments
	name       string // for :name or *name (without prefix)
	isParam    bool
	isWildcard bool
}

// compilePattern converts a route pattern into a slice of compiledSegs.
// It avoids allocation on request paths by doing this work at registration time.
func compilePattern(p string) []compiledSeg {
	if p == "" || p == "/" {
		return nil
	}
	pattern := p
	if p[0] == '/' {
		p = p[1:]
	}
	if p == "" {
		return nil
	}
	parts := strings.Split(p, "/")
	out := make([]compiledSeg, 0, len(parts))
	for _, s := range parts {
		if s == "" {
			continue
		}
		if s[0] == ':' {
			if len(s) == 1 {
				panic("route parameter name cannot be empty: " + pattern)
			}
			out = append(out, compiledSeg{isParam: true, name: s[1:]})
			continue
		}
		if s[0] == '*' {
			if len(s) == 1 {
				panic("route wildcard name cannot be empty: " + pattern)
			}
			out = append(out, compiledSeg{isWildcard: true, name: s[1:]})
			continue
		}
		out = append(out, compiledSeg{literal: s})
	}
	return out
}

// pathIter yields each segment of a URL path without allocating []string.
// It returns slices referencing the original path string.
type pathIter struct {
	path string
	i    int
	n    int
}

func newPathIter(p string) pathIter {
	return pathIter{path: p, i: 0, n: len(p)}
}

// next returns the next segment and a boolean indicating availability.
func (it *pathIter) next() (string, bool) {
	// Skip leading slashes
	for it.i < it.n && it.path[it.i] == '/' {
		it.i++
	}
	if it.i >= it.n {
		return "", false
	}
	start := it.i
	// Find the end of the segment
	for it.i < it.n && it.path[it.i] != '/' {
		it.i++
	}
	return it.path[start:it.i], true
}

// tail returns the current segment plus the remainder of the path joined by '/'.
// This is used for wildcard matches.
func (it *pathIter) tail(currentSeg string) string {
	// If we're already at the end, return current segment.
	if it.i >= it.n {
		return currentSeg
	}
	// Skip the slash between current and next
	if it.path[it.i] == '/' {
		return currentSeg + it.path[it.i:]
	}
	return currentSeg + "/" + it.path[it.i:]
}
