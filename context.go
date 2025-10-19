package zentrox

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
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

	"github.com/aminofox/zentrox/binding"
	"github.com/aminofox/zentrox/validation"
)

// Context carries request-scoped values and the middleware/handler chain.
type Context struct {
	Writer  http.ResponseWriter
	Request *http.Request
	params  map[string]string
	index   int
	stack   []Handler
	store   map[string]any

	aborted bool  // whether the chain has been stopped
	err     error // last error recorded for this request (if any)
}

// Forward runs the next middleware/handler in the chain.
func (c *Context) Forward() {
	c.index++
	for c.index < len(c.stack) {
		c.stack[c.index](c)
		if c.aborted {
			return
		}
		c.index++
	}
}

// Abort stops the chain immediately (no response is written automatically).
func (c *Context) Abort() {
	c.aborted = true
}

// Aborted reports whether the chain was already stopped.
func (c *Context) Aborted() bool {
	return c.aborted
}

// Fail sends a standardized HTTPError JSON and stops the chain.
func (c *Context) Fail(code int, message string, detail ...any) {
	c.err = NewHTTPError(code, message, detail...)
	c.SendJSON(code, c.err)
	c.Abort()
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
	return c.GetHeader(key)
}

// Set stores an arbitrary value for the lifetime of the request.
func (c *Context) Set(key string, v any) {
	c.store[key] = v
}

// Get retrieves a value previously stored with Set.
func (c *Context) Get(key string) (any, bool) {
	v, ok := c.store[key]
	return v, ok
}

// Binding & Validation
// BindInto auto-detects the binder (JSON/Form/Query), binds into dst, then validates tags.
func (c *Context) BindInto(dst any) error {
	if err := binding.Bind(c.Request, dst); err != nil {
		return err
	}
	return validation.ValidateStruct(dst)
}

// BindJSONInto binds JSON into dst and validates tags.
func (c *Context) BindJSONInto(dst any) error {
	if err := binding.JSON.Bind(c.Request, dst); err != nil {
		return err
	}
	return validation.ValidateStruct(dst)
}

// BindFormInto binds form data into dst and validates tags.
func (c *Context) BindFormInto(dst any) error {
	if err := binding.Form.Bind(c.Request, dst); err != nil {
		return err
	}
	return validation.ValidateStruct(dst)
}

// BindQueryInto binds query params into dst and validates tags.
func (c *Context) BindQueryInto(dst any) error {
	if err := binding.Query.Bind(c.Request, dst); err != nil {
		return err
	}
	return validation.ValidateStruct(dst)
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

func (c *Context) SendJSON(code int, v any) {
	c.Writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	c.Writer.WriteHeader(code)

	enc := json.NewEncoder(c.Writer)
	enc.SetEscapeHTML(false) // do not escape < > & by default; safer for API payloads

	if err := enc.Encode(v); err != nil {
		// Fallback to a minimal error envelope if marshaling fails
		_, _ = c.Writer.Write([]byte(`{"code":500,"message":"json encode failed"}`))
	}
}

func (c *Context) SendText(code int, s string) {
	c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.Writer.WriteHeader(code)
	_, _ = c.Writer.Write([]byte(s))
}

func (c *Context) SendHTML(code int, html string) {
	c.Writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	c.Writer.WriteHeader(code)
	_, _ = c.Writer.Write([]byte(html))
}

func (c *Context) SendXML(code int, v any) {
	c.Writer.Header().Set("Content-Type", "application/xml; charset=utf-8")
	c.Writer.WriteHeader(code)
	b, err := xml.Marshal(v)
	if err != nil {
		_, _ = c.Writer.Write([]byte("<error>xml marshal failed</error>"))
		return
	}
	_, _ = c.Writer.Write(b)
}

func (c *Context) SendData(code int, contentType string, b []byte) {
	if contentType != "" {
		c.Writer.Header().Set("Content-Type", contentType)
	}
	c.Writer.WriteHeader(code)
	_, _ = c.Writer.Write(b)
}

func (c *Context) SendFile(path string) {
	http.ServeFile(c.Writer, c.Request, path)
}

func (c *Context) SendAttachment(path, filename string) {
	if filename == "" {
		filename = filepath.Base(path)
	}
	c.Writer.Header().Set("Content-Disposition", "attachment; filename="+filename)
	f, err := os.Open(path)
	if err != nil {
		c.SendText(http.StatusNotFound, "file not found")
		return
	}
	defer f.Close()

	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	ct := http.DetectContentType(buf[:n])
	c.Writer.Header().Set("Content-Type", ct)
	_, _ = f.Seek(0, 0)

	c.Writer.WriteHeader(http.StatusOK)
	_, _ = io.Copy(c.Writer, f)
}

func (c *Context) SendBytes(code int, b []byte) {
	c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.Writer.WriteHeader(code)
	_, _ = c.Writer.Write(b)
}

func (c *Context) SendStatus(code int) {
	c.Writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	c.Writer.WriteHeader(code)
}

func (c *Context) PushStream(fn func(w io.Writer, flush func())) {
	c.Writer.Header().Set("Content-Type", "application/octet-stream")
	c.Writer.WriteHeader(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)
	flush := func() {
		if flusher != nil {
			flusher.Flush()
		}
	}
	fn(c.Writer, flush)
}

func (c *Context) PushSSE(fn func(event func(name, data string))) {
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.WriteHeader(http.StatusOK)

	flusher, _ := c.Writer.(http.Flusher)
	event := func(name, data string) {
		_, _ = io.WriteString(c.Writer, "event: "+name+"\n")
		_, _ = io.WriteString(c.Writer, "data: "+data+"\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}
	fn(event)
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
	r := c.Request
	// X-Forwarded-For could be "client, proxy1, proxy2"
	if v := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); v != "" {
		if i := strings.IndexByte(v, ','); i >= 0 {
			return strings.TrimSpace(v[:i])
		}
		return v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
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
		name = base + "-" + time.Now().UTC().Format("20060102T150405") + ext
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

	// Prevent path traversal
	target := filepath.Join(dstDir, filepath.Base(name))
	if ok := isWithinBase(dstDir, target); !ok { // reuse helper from zentrox.go
		return "", errors.New("upload: invalid path")
	}

	// Create directory tree if needed
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}

	// Deny overwrite unless allowed
	if !opt.Overwrite {
		if _, err := os.Stat(target); err == nil {
			return "", errors.New("upload: file exists")
		}
	}

	// Copy stream to disk (0600 for privacy by default)
	dst, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, file); err != nil {
		return "", err
	}

	return target, nil
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

// sanitizeFilename strips unsupported characters from a file name.
func sanitizeFilename(name string) string {
	name = filepath.Base(name)
	// Allow letters, digits, dot, underscore, hyphen
	allow := regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	name = allow.ReplaceAllString(name, "_")
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
	accept := c.GetHeader("Accept")
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
		c.SendText(code, "")
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
	case "application/json", "application/problem+json":
		c.SendJSON(code, payload)
	case "text/plain":
		if s, ok := payload.(string); ok {
			c.SendText(code, s)
		} else {
			c.SendText(code, "")
		}
	case "text/html":
		if s, ok := payload.(string); ok {
			c.SendHTML(code, s)
		} else {
			c.SendHTML(code, "")
		}
	case "application/xml", "text/xml":
		c.SendXML(code, payload)
	default:
		// Fallback to JSON if provided, else first candidate as text
		if v, ok := candidates["application/json"]; ok {
			c.SendJSON(code, v)
			return
		}
		// Try to stringify the first candidate if it is string
		first := keys[0]
		if s, ok := candidates[first].(string); ok {
			c.SendText(code, s)
			return
		}
		// Otherwise just JSON the first candidate
		c.SendJSON(code, candidates[first])
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
func (c *Context) Problem(status int, typeURI, title, detail, instance string, ext map[string]any) {
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
	c.Writer.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	c.Writer.WriteHeader(status)
	enc := json.NewEncoder(c.Writer)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(p); err != nil {
		_, _ = c.Writer.Write([]byte(`{"type":"about:blank","title":"Internal Server Error","status":500}`))
	}
}

// Problemf is a convenience helper to write a simple problem without instance/ext.
func (c *Context) Problemf(status int, title string, detail string) {
	c.Problem(status, "about:blank", title, detail, "", nil)
}
