package zentrox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/aminofox/zentrox/v2/validation"
)

// Context carries request-scoped values and the middleware/handler chain.
type Context struct {
	Writer  http.ResponseWriter
	Request *http.Request
	params  map[string]string
	index   int
	stack   []Handler
	store   map[any]any
	realIP  func(*http.Request) string
	route   string

	aborted bool
	err     error

	responseCommitted bool
	validator         validation.StructValidator
	jsonCodec         JSONCodec
}

// ErrResponseCommitted is returned by response helpers when headers were already written.
var ErrResponseCommitted = errors.New("response already committed")

// JSONCodec serializes and deserializes JSON payloads for Context.JSON and BindJSONInto.
type JSONCodec interface {
	Marshal(v any) ([]byte, error)
	Unmarshal(data []byte, v any) error
}

type defaultJSONCodec struct{}

// DefaultJSONCodec returns Zentrox's encoding/json based codec.
func DefaultJSONCodec() JSONCodec {
	return defaultJSONCodec{}
}

func (defaultJSONCodec) Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (defaultJSONCodec) Unmarshal(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("multiple JSON values not allowed")
		}
		return err
	}
	return nil
}

type jsonCodecFuncs struct {
	marshal   func(any) ([]byte, error)
	unmarshal func([]byte, any) error
}

func (f jsonCodecFuncs) Marshal(v any) ([]byte, error) {
	return f.marshal(v)
}

func (f jsonCodecFuncs) Unmarshal(data []byte, v any) error {
	return f.unmarshal(data, v)
}

// ResponseCommitted reports whether the response status has already been written.
func (c *Context) ResponseCommitted() bool {
	if c.responseCommitted {
		return true
	}
	rw, ok := c.Writer.(interface{ Status() int })
	return ok && rw.Status() != 0
}

func (c *Context) markResponseCommitted() {
	c.responseCommitted = true
}

// Next executes the next handler in the middleware chain.
func (c *Context) Next() {
	if c.index >= len(c.stack) {
		return
	}
	c.index++
	for c.index < len(c.stack) {
		if c.aborted {
			return
		}
		c.stack[c.index](c)
		c.index++
	}
}

// Abort stops the middleware chain.
func (c *Context) Abort() {
	c.aborted = true
}

// Aborted returns true if the chain was aborted.
func (c *Context) Aborted() bool {
	return c.aborted
}

// Fail sends a standardized HTTPError JSON and stops the chain.
func (c *Context) Fail(code int, message string, detail ...any) error {
	c.err = NewHTTPError(code, message, detail...)
	err := c.JSON(code, c.err)
	c.Abort()
	return err
}

// Error returns the last recorded error, if any.
func (c *Context) Error() error {
	return c.err
}

// SetError records an error for the request (ErrorHandler can render it later).
func (c *Context) SetError(err error) {
	c.err = err
}

// ClearError clears the last recorded error.
func (c *Context) ClearError() {
	c.err = nil
}

// Param returns a path parameter value.
func (c *Context) Param(key string) string {
	return c.params[key]
}

// Query returns a query parameter value.
func (c *Context) Query(key string) string {
	return c.Request.URL.Query().Get(key)
}

// SetHeader sets a response header.
func (c *Context) SetHeader(k, v string) {
	c.Writer.Header().Set(k, v)
}

// GetHeader returns the first value of the specified request header.
func (c *Context) GetHeader(key string) string {
	return c.Request.Header.Get(key)
}

// RoutePath returns the matched route template, such as "/users/:id".
// It is empty when no route matched.
func (c *Context) RoutePath() string {
	return c.route
}

// Set stores an arbitrary value for the lifetime of the request.
func (c *Context) Set(key any, v any) {
	if c.store == nil {
		c.store = make(map[any]any)
	}
	c.store[key] = v
}

// Get retrieves a value previously stored with Set.
func (c *Context) Get(key any) (any, bool) {
	if c.store == nil {
		return nil, false
	}
	v, ok := c.store[key]
	return v, ok
}

// Copy returns a shallow copy of the Context that is safe to use outside the request scope.
// It copies the store, Request, and params, but does NOT copy the ResponseWriter or middleware stack.
// Use this if you need to pass Context data to a background goroutine.
func (c *Context) Copy() *Context {
	var req *http.Request
	if c.Request != nil {
		req = c.Request.Clone(context.Background())
	}
	cp := &Context{
		Request: req,
		params:  make(map[string]string, len(c.params)),
		store:   make(map[any]any, len(c.store)),
		realIP:  c.realIP,
		route:   c.route,
	}
	for k, v := range c.params {
		cp.params[k] = v
	}
	for k, v := range c.store {
		cp.store[k] = v
	}
	return cp
}

// RequestID returns the request ID if a RequestID middleware has stored it.
func (c *Context) RequestID() string {
	if v, ok := c.Get(RequestID); ok {
		if s, _ := v.(string); s != "" {
			return s
		}
	}
	return ""
}

// Deadline returns the time when work done on behalf of this request
// should be canceled. It proxies http.Request.Context().
func (c *Context) Deadline() (time.Time, bool) {
	if c.Request == nil {
		return time.Time{}, false
	}
	return c.Request.Context().Deadline()
}

// Done returns a channel that is closed when the request context is canceled.
// It proxies http.Request.Context().
func (c *Context) Done() <-chan struct{} {
	if c.Request == nil {
		return nil
	}
	return c.Request.Context().Done()
}

// Err reports why the request context was canceled, if it was.
// It proxies http.Request.Context().
func (c *Context) Err() error {
	if c.Request == nil {
		return nil
	}
	return c.Request.Context().Err()
}

// Value implements context.Context and returns the value associated with this context for key.
// Proxies http.Request.Context().Value(key).
func (c *Context) Value(key any) any {
	if c.Request == nil {
		return nil
	}
	return c.Request.Context().Value(key)
}

// RealIP returns the client IP considering configured reverse proxy settings.
// When trusted proxies are configured on the app, it uses that evaluation.
// Otherwise, it falls back safely to RemoteAddr without trusting spoofable headers.
func (c *Context) RealIP() string {
	if c.Request == nil {
		return ""
	}
	if c.realIP != nil {
		return c.realIP(c.Request)
	}
	ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return c.Request.RemoteAddr
	}
	return ip
}
