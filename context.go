package zentrox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"

	"strconv"
	"strings"
	"time"

	"github.com/aminofox/zentrox/v2/binding"
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

// Next executes the next handler in the middleware chain
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

// Abort stops the middleware chain
func (c *Context) Abort() {
	c.aborted = true
}

// Aborted returns true if the chain was aborted
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

// Binding & Validation
// BindInto auto-detects the binder (JSON/Form/Query), binds into dst, then validates tags.
func (c *Context) BindInto(dst any) error {
	if err := binding.Bind(c.Request, dst); err != nil {
		return err
	}
	return c.validateStruct(dst)
}

// BindJSONInto binds JSON into dst and validates tags.
func (c *Context) BindJSONInto(dst any) error {
	if err := c.bindJSONInto(dst); err != nil {
		return err
	}
	return c.validateStruct(dst)
}

// BindStrictJSONInto binds JSON into dst, rejects unknown fields and extra JSON documents, then validates tags.
func (c *Context) BindStrictJSONInto(dst any) error {
	if err := binding.StrictJSON.Bind(c.Request, dst); err != nil {
		return err
	}
	return c.validateStruct(dst)
}

// BindFormInto binds form data into dst and validates tags.
func (c *Context) BindFormInto(dst any) error {
	if err := binding.Form.Bind(c.Request, dst); err != nil {
		return err
	}
	return c.validateStruct(dst)
}

// BindQueryInto binds query params into dst and validates tags.
func (c *Context) BindQueryInto(dst any) error {
	if err := binding.Query.Bind(c.Request, dst); err != nil {
		return err
	}
	return c.validateStruct(dst)
}

func (c *Context) validateStruct(dst any) error {
	if c.validator != nil {
		return c.validator.ValidateStruct(dst)
	}
	return validation.ValidateStruct(dst)
}

func (c *Context) bindJSONInto(dst any) error {
	if c.jsonCodec == nil {
		return binding.JSON.Bind(c.Request, dst)
	}
	if c.Request.Body == nil {
		return errors.New("empty body")
	}
	defer c.Request.Body.Close()
	b, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return err
	}
	return c.jsonCodec.Unmarshal(b, dst)
}

// BindHeaderInto maps request headers into a struct.
// Tag: `header:"X-Trace-Id,required"` ; if no tag -> use Canonical(FieldName).
func (c *Context) BindHeaderInto(dst any) error {
	if dst == nil {
		return fmt.Errorf("BindHeaderInto: dst is nil")
	}
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fmt.Errorf("BindHeaderInto: dst must be non-nil pointer to struct")
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("BindHeaderInto: dst must point to struct")
	}

	h := c.Request.Header

	for i := 0; i < v.NumField(); i++ {
		sf := v.Type().Field(i)
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get("header")
		name, required := parseHeaderTag(tag, textproto.CanonicalMIMEHeaderKey(sf.Name))
		if name == "-" {
			continue
		}

		vals := h.Values(name)
		if len(vals) == 0 || (len(vals) == 1 && vals[0] == "") {
			if required {
				return fmt.Errorf("BindHeaderInto: missing required header %q", name)
			}
			continue
		}

		fv := v.Field(i)
		if fv.Kind() == reflect.Slice && fv.Type().Elem().Kind() == reflect.String {
			fv.Set(reflect.ValueOf(vals))
			continue
		}

		// get first header if not[]string
		raw := vals[0]
		if err := setField(fv, raw); err != nil {
			return fmt.Errorf("BindHeaderInto: field %s: %w", sf.Name, err)
		}
	}
	return nil
}

// BindPathInto maps path params (zentrox params) into a struct.
// Tag: `path:"id,required"` ; if no tag -> use lowerCamel(FldName).
func (c *Context) BindPathInto(dst any) error {
	if dst == nil {
		return fmt.Errorf("BindPathInto: dst is nil")
	}
	v := reflect.ValueOf(dst)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return fmt.Errorf("BindPathInto: dst must be non-nil pointer to struct")
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("BindPathInto: dst must point to struct")
	}

	for i := 0; i < v.NumField(); i++ {
		sf := v.Type().Field(i)
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get("path")
		name, required := parseTagNameRequired(tag, lowerCamel(sf.Name))
		raw, ok := c.params[name]
		if !ok || raw == "" {
			if required {
				return fmt.Errorf("BindPathInto: missing required path param %q", name)
			}
			continue
		}
		if err := setField(v.Field(i), raw); err != nil {
			return fmt.Errorf("BindPathInto: field %s: %w", sf.Name, err)
		}
	}
	return nil
}

func lowerCamel(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func parseTagNameRequired(tag, fallback string) (name string, required bool) {
	if tag == "" {
		return fallback, false
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = fallback
	}
	for _, p := range parts[1:] {
		if strings.TrimSpace(p) == "required" {
			required = true
		}
	}
	return
}

func setField(fv reflect.Value, s string) error {
	if !fv.CanSet() {
		return fmt.Errorf("cannot set")
	}
	ft := fv.Type()
	switch ft.Kind() {
	case reflect.String:
		fv.SetString(s)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		fv.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		i, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		fv.SetInt(i)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return err
		}
		fv.SetUint(u)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return err
		}
		fv.SetFloat(f)
	default:
		return fmt.Errorf("unsupported kind %s", ft.Kind())
	}
	return nil
}

func parseHeaderTag(tag, fallback string) (name string, required bool) {
	if tag == "" {
		return fallback, false
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if name == "" {
		name = fallback
	}
	for _, p := range parts[1:] {
		if strings.TrimSpace(p) == "required" {
			required = true
		}
	}
	return
}

// JSON sends a JSON response
func (c *Context) JSON(code int, v any) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	codec := c.jsonCodec
	if codec == nil {
		codec = DefaultJSONCodec()
	}
	b, err := codec.Marshal(v)
	if err != nil {
		return err
	}
	c.Writer.Header().Set(HeaderContentType, ContentTypeJSONUTF8)
	c.markResponseCommitted()
	c.Writer.WriteHeader(code)
	_, err = c.Writer.Write(b)
	return err
}

// String sends a plain text response
func (c *Context) String(code int, format string, values ...any) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	c.Writer.Header().Set(HeaderContentType, ContentTypeTextUTF8)
	c.markResponseCommitted()
	c.Writer.WriteHeader(code)
	if len(values) > 0 {
		_, err := fmt.Fprintf(c.Writer, format, values...)
		return err
	}
	_, err := c.Writer.Write([]byte(format))
	return err
}

// HTML sends an HTML response
func (c *Context) HTML(code int, html string) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	c.Writer.Header().Set(HeaderContentType, ContentTypeHTMLUTF8)
	c.markResponseCommitted()
	c.Writer.WriteHeader(code)
	_, err := c.Writer.Write([]byte(html))
	return err
}

// XML sends an XML response
func (c *Context) XML(code int, v any) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	c.Writer.Header().Set(HeaderContentType, ContentTypeXMLUTF8)
	c.markResponseCommitted()
	c.Writer.WriteHeader(code)
	b, err := xml.Marshal(v)
	if err != nil {
		_, _ = c.Writer.Write([]byte("<error>xml marshal failed</error>"))
		return err
	}
	_, err = c.Writer.Write(b)
	return err
}

// Data sends raw bytes with custom content type
func (c *Context) Data(code int, contentType string, b []byte) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	if contentType != "" {
		c.Writer.Header().Set(HeaderContentType, contentType)
	}
	c.markResponseCommitted()
	c.Writer.WriteHeader(code)
	_, err := c.Writer.Write(b)
	return err
}

func (c *Context) Download(filepath string, filename string) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	if filename != "" {
		c.Writer.Header().Set(HeaderContentDisposition, mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	}
	c.markResponseCommitted()
	http.ServeFile(c.Writer, c.Request, filepath)
	return nil
}

func (c *Context) SendAttachment(path, filename string) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	if filename == "" {
		filename = filepath.Base(path)
	}
	c.Writer.Header().Set(HeaderContentDisposition, mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	f, err := os.Open(path)
	if err != nil {
		_ = c.String(http.StatusNotFound, MsgFileNotFound)
		return err
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return err
	}
	ct := http.DetectContentType(buf[:n])
	c.Writer.Header().Set(HeaderContentType, ct)
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}

	c.markResponseCommitted()
	c.Writer.WriteHeader(http.StatusOK)
	_, err = io.Copy(c.Writer, f)
	return err
}

func (c *Context) SendBytes(code int, b []byte) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	c.Writer.Header().Set(HeaderContentType, ContentTypeTextUTF8)
	c.markResponseCommitted()
	c.Writer.WriteHeader(code)
	_, err := c.Writer.Write(b)
	return err
}

func (c *Context) SendStatus(code int) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	c.Writer.Header().Set(HeaderContentType, ContentTypeTextUTF8)
	c.markResponseCommitted()
	c.Writer.WriteHeader(code)
	return nil
}

func (c *Context) PushStream(fn func(w io.Writer, flush func())) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return http.ErrNotSupported
	}
	c.Writer.Header().Set(HeaderContentType, ContentTypeOctetStream)
	c.markResponseCommitted()
	c.Writer.WriteHeader(http.StatusOK)
	flush := func() {
		flusher.Flush()
	}
	fn(c.Writer, flush)
	return nil
}

func (c *Context) PushSSE(fn func(event func(name, data string))) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return http.ErrNotSupported
	}
	c.Writer.Header().Set(HeaderContentType, ContentTypeEventStream)
	c.Writer.Header().Set(HeaderCacheControl, CacheControlNoCache)
	c.markResponseCommitted()
	c.Writer.WriteHeader(http.StatusOK)

	var firstErr error
	event := func(name, data string) {
		if firstErr != nil {
			return
		}
		name = sanitizeSSEEventName(name)
		if _, firstErr = io.WriteString(c.Writer, "event: "+name+"\n"); firstErr != nil {
			return
		}
		data = normalizeSSEData(data)
		for _, line := range strings.Split(data, "\n") {
			if _, firstErr = io.WriteString(c.Writer, "data: "+line+"\n"); firstErr != nil {
				return
			}
		}
		_, firstErr = io.WriteString(c.Writer, "\n")
		if firstErr != nil {
			return
		}
		flusher.Flush()
	}
	fn(event)
	return firstErr
}

func sanitizeSSEEventName(name string) string {
	name = strings.ReplaceAll(name, "\r", "")
	return strings.ReplaceAll(name, "\n", "")
}

func normalizeSSEData(data string) string {
	data = strings.ReplaceAll(data, "\r\n", "\n")
	return strings.ReplaceAll(data, "\r", "\n")
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

// Value implements context.Context and returns the value associated with this context for key,required Go 1.18+.
// Proxies http.Request.Context().Value(key).
func (c *Context) Value(key any) any {
	if c.Request == nil {
		return nil
	}
	return c.Request.Context().Value(key)
}

// RealIP returns the client IP considering common reverse proxy headers.
// Order: X-Forwarded-For (first), X-Real-IP, then RemoteAddr fallback.
func (c *Context) RealIP() string {
	if c.Request == nil {
		return ""
	}
	if c.realIP != nil {
		return c.realIP(c.Request)
	}
	r := c.Request
	// X-Forwarded-For could be "client, proxy1, proxy2"
	if v := strings.TrimSpace(r.Header.Get(HeaderXForwardedFor)); v != "" {
		if i := strings.IndexByte(v, ','); i >= 0 {
			return strings.TrimSpace(v[:i])
		}
		return v
	}
	if v := strings.TrimSpace(r.Header.Get(HeaderXRealIP)); v != "" {
		return v
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

// UploadOptions controls how files are accepted and saved.
type UploadOptions struct {
	// Maximum memory used by ParseMultipartForm; files larger than this are stored in temporary files.
	MaxMemory int64 // default 10 << 20 (10 MiB)
	// Allowed file extensions (lowercase, with dot). Empty means allow all.
	AllowedExt []string
	// If true, sanitize the base filename (only [a-zA-Z0-9._-]) to avoid weird characters.
	Sanitize bool
	// If true, always generate a unique filename (timestamp + random suffix).
	GenerateUniqueName bool
	// If false and file exists, returns error. If true, overwrite existing file.
	Overwrite bool
}

// SaveUploadedFile reads file from multipart form by field name and writes it into dstDir.
// It validates extension (if provided), prevents path traversal, and can sanitize/generate names.
// Returns the full path saved to.
func (c *Context) SaveUploadedFile(field, dstDir string, opt UploadOptions) (string, error) {
	if dstDir == "" {
		return "", errors.New("upload: destination directory required")
	}
	if opt.MaxMemory <= 0 {
		opt.MaxMemory = 10 << 20 // 10 MiB
	}
	if err := c.Request.ParseMultipartForm(opt.MaxMemory); err != nil {
		return "", err
	}
	file, hdr, err := c.Request.FormFile(field)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Decide target filename
	name := hdr.Filename
	if opt.Sanitize {
		name = sanitizeFilename(name)
	}
	if opt.GenerateUniqueName {
		ext := strings.ToLower(filepath.Ext(name))
		base := strings.TrimSuffix(name, ext)
		name = base + "-" + time.Now().UTC().Format("20060102T150405") + "-" + randomHex(4) + ext
	}
	if name == "" {
		return "", errors.New("upload: empty filename")
	}

	// Extension allow-list
	if len(opt.AllowedExt) > 0 {
		ext := strings.ToLower(filepath.Ext(name))
		allowed := false
		for _, e := range opt.AllowedExt {
			if strings.ToLower(e) == ext {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", errors.New("upload: disallowed file extension")
		}
	}

	dstRoot, err := filepath.Abs(dstDir)
	if err != nil {
		return "", err
	}

	// Prevent path traversal
	target := filepath.Join(dstRoot, filepath.Base(name))
	if ok := isWithinBase(dstRoot, target); !ok { // reuse helper from zentrox.go
		return "", errors.New("upload: invalid path")
	}

	// Create directory tree if needed
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	realDstRoot, err := filepath.EvalSymlinks(dstRoot)
	if err != nil {
		return "", err
	}
	realTargetDir, err := filepath.EvalSymlinks(filepath.Dir(target))
	if err != nil {
		return "", err
	}
	if !isWithinBase(realDstRoot, realTargetDir) {
		return "", errors.New("upload: invalid destination")
	}

	// Deny overwrite unless allowed
	if fi, err := os.Lstat(target); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("upload: refusing to overwrite symlink")
		}
		if fi.IsDir() {
			return "", errors.New("upload: target is a directory")
		}
		if !opt.Overwrite {
			return "", errors.New("upload: file exists")
		}
		if err := os.Remove(target); err != nil {
			return "", err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	// Copy stream to disk (0600 for privacy by default)
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		return "", err
	}

	return target, nil
}

func randomHex(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "random"
	}
	return hex.EncodeToString(b)
}

// UploadedFile returns the multipart file and header for advanced use.
// Caller must close the returned multipart.File.
func (c *Context) UploadedFile(field string, maxMemory int64) (multipart.File, *multipart.FileHeader, error) {
	if maxMemory <= 0 {
		maxMemory = 10 << 20
	}
	if err := c.Request.ParseMultipartForm(maxMemory); err != nil {
		return nil, nil, err
	}
	return c.Request.FormFile(field)
}

var sanitizeFilenameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

// sanitizeFilename strips unsupported characters from a file name.
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	name = sanitizeFilenameRe.ReplaceAllString(name, "_")
	// Avoid empty name
	if name == "" || name == "." || name == ".." {
		name = "file"
	}
	return name
}

// Accepts returns the preferred type among provided candidates according to the
// request's "Accept" header. It returns the first element of candidates if the header
// is empty or no match is found.
func (c *Context) Accepts(candidates ...string) string {
	if len(candidates) == 0 {
		return ""
	}
	accept := c.GetHeader(HeaderAccept)
	if strings.TrimSpace(accept) == "" {
		return candidates[0]
	}
	// Parse Accept with q-values and order by quality then by original order.
	var prefs []acceptSpec
	for _, part := range strings.Split(accept, ",") {
		as := parseAcceptSpec(strings.TrimSpace(part))
		if as.value != "" {
			prefs = append(prefs, as)
		}
	}
	if len(prefs) == 0 {
		return candidates[0]
	}

	// Match by exact type/subtype, then type/*, then */*.
	for _, p := range prefs {
		for _, cand := range candidates {
			if matchesMedia(p.value, cand) {
				return cand
			}
		}
	}
	// No match -> fall back
	return candidates[0]
}

type acceptSpec struct {
	value string
	q     float64
	i     int // original order to keep stable sort for equal q
}

func parseAcceptSpec(s string) acceptSpec {
	as := acceptSpec{value: s, q: 1.0}
	// Split parameters
	parts := strings.Split(s, ";")
	as.value = strings.TrimSpace(parts[0])
	as.i = 0
	for _, p := range parts[1:] {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "q=") {
			if v, err := strconv.ParseFloat(strings.TrimPrefix(p, "q="), 64); err == nil {
				as.q = v
			}
		}
	}
	return as
}

func matchesMedia(acceptVal, candidate string) bool {
	// acceptVal can be */*, type/*, or type/subtype
	av := strings.TrimSpace(strings.ToLower(acceptVal))
	cv := strings.TrimSpace(strings.ToLower(candidate))
	if av == "*/*" || av == cv {
		return true
	}
	// type/* pattern
	if strings.HasSuffix(av, "/*") {
		return strings.HasPrefix(cv, strings.TrimSuffix(av, "*"))
	}
	return false
}

// Negotiate writes the response based on the request's Accept header.
// candidates is a map of content-type -> payload.
// Supported types out-of-the-box:
//   - "application/json": payload marshaled as JSON (via SendJSON)
//   - "text/plain": payload must be string
//   - "text/html": payload must be string (HTML)
//   - "application/xml": payload marshaled as XML (via SendXML)
//
// Example:
//
//	c.Negotiate(200, map[string]any{
//	  "application/json": obj,
//	  "text/plain":       "hello",
//	})
func (c *Context) Negotiate(code int, candidates map[string]any) {
	if len(candidates) == 0 {
		c.String(code, "")
		return
	}
	// Keep a stable list of candidate types to use as fallback order
	keys := make([]string, 0, len(candidates))
	for k := range candidates {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	ct := c.Accepts(keys...)
	payload := candidates[ct]

	switch ct {
	case ContentTypeJSON, ContentTypeProblemJSON:
		c.JSON(code, payload)
	case "text/plain":
		if s, ok := payload.(string); ok {
			c.String(code, "%s", s)
		} else {
			c.String(code, "")
		}
	case "text/html":
		if s, ok := payload.(string); ok {
			c.HTML(code, s)
		} else {
			c.HTML(code, "")
		}
	case "application/xml", "text/xml":
		c.XML(code, payload)
	default:
		// Fallback to JSON if provided, else first candidate as text
		if v, ok := candidates[ContentTypeJSON]; ok {
			c.JSON(code, v)
			return
		}
		// Try to stringify the first candidate if it is string
		first := keys[0]
		if s, ok := candidates[first].(string); ok {
			c.String(code, "%s", s)
			return
		}
		// Otherwise just JSON the first candidate
		c.JSON(code, candidates[first])
	}
}

// Problem is a serializable RFC 9457 error object. Extension members are
// included when marshaled by merging Ext into the base object.
type Problem struct {
	Type     string         `json:"type,omitempty"`     // A URI reference that identifies the problem type
	Title    string         `json:"title,omitempty"`    // A short, human-readable summary of the problem type
	Status   int            `json:"status,omitempty"`   // HTTP status code generated by the origin server
	Detail   string         `json:"detail,omitempty"`   // Human-readable explanation specific to this occurrence
	Instance string         `json:"instance,omitempty"` // A URI reference that identifies the specific occurrence
	Ext      map[string]any `json:"-"`                  // extension members
}

// MarshalJSON merges extension members into the base JSON.
func (p Problem) MarshalJSON() ([]byte, error) {
	base := map[string]any{}
	if p.Type != "" {
		base["type"] = p.Type
	}
	if p.Title != "" {
		base["title"] = p.Title
	}
	if p.Status != 0 {
		base["status"] = p.Status
	}
	if p.Detail != "" {
		base["detail"] = p.Detail
	}
	if p.Instance != "" {
		base["instance"] = p.Instance
	}
	for k, v := range p.Ext {
		// do not override base keys
		if _, exists := base[k]; !exists {
			base[k] = v
		}
	}
	return json.Marshal(base)
}

// Problem writes an application/problem+json response using the provided fields.
// The Content-Type is set to "application/problem+json".
func (c *Context) Problem(status int, typeURI, title, detail, instance string, ext map[string]any) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	if ext == nil {
		ext = map[string]any{}
	}
	p := Problem{
		Type:     typeURI,
		Title:    title,
		Status:   status,
		Detail:   detail,
		Instance: instance,
		Ext:      ext,
	}
	// Explicit content-type per RFC
	c.Writer.Header().Set(HeaderContentType, ContentTypeProblemJSONUTF8)
	c.markResponseCommitted()
	c.Writer.WriteHeader(status)
	enc := json.NewEncoder(c.Writer)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		_, _ = c.Writer.Write([]byte(`{"type":"about:blank","title":"Internal Server Error","status":500}`))
		return err
	}
	return nil
}

// Problemf is a convenience helper to write a simple problem without instance/ext.
func (c *Context) Problemf(status int, title string, detail string) error {
	return c.Problem(status, "about:blank", title, detail, "", nil)
}
