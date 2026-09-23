package zentrox

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// JSON sends a JSON response.
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
	c.Writer.Header().Set(HeaderXContentTypeOptions, "nosniff")
	c.markResponseCommitted()
	c.Writer.WriteHeader(code)
	_, err = c.Writer.Write(b)
	return err
}

// String sends a plain text response.
func (c *Context) String(code int, format string, values ...any) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	c.Writer.Header().Set(HeaderContentType, ContentTypeTextUTF8)
	c.Writer.Header().Set(HeaderXContentTypeOptions, "nosniff")
	c.markResponseCommitted()
	c.Writer.WriteHeader(code)
	if len(values) > 0 {
		_, err := fmt.Fprintf(c.Writer, format, values...)
		return err
	}
	_, err := c.Writer.Write([]byte(format))
	return err
}

// HTML sends an HTML response.
func (c *Context) HTML(code int, html string) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	c.Writer.Header().Set(HeaderContentType, ContentTypeHTMLUTF8)
	c.Writer.Header().Set(HeaderXContentTypeOptions, "nosniff")
	c.markResponseCommitted()
	c.Writer.WriteHeader(code)
	_, err := c.Writer.Write([]byte(html))
	return err
}

// XML sends an XML response.
func (c *Context) XML(code int, v any) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	c.Writer.Header().Set(HeaderContentType, ContentTypeXMLUTF8)
	c.Writer.Header().Set(HeaderXContentTypeOptions, "nosniff")
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

// Data sends raw bytes with custom content type.
func (c *Context) Data(code int, contentType string, b []byte) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	if contentType != "" {
		c.Writer.Header().Set(HeaderContentType, contentType)
	}
	c.Writer.Header().Set(HeaderXContentTypeOptions, "nosniff")
	c.markResponseCommitted()
	c.Writer.WriteHeader(code)
	_, err := c.Writer.Write(b)
	return err
}

// Download serves a local file as an attachment.
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

// SendAttachment streams a local file with attachment header.
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

// SendBytes sends raw bytes with text/plain content type.
func (c *Context) SendBytes(code int, b []byte) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	c.Writer.Header().Set(HeaderContentType, ContentTypeTextUTF8)
	c.Writer.Header().Set(HeaderXContentTypeOptions, "nosniff")
	c.markResponseCommitted()
	c.Writer.WriteHeader(code)
	_, err := c.Writer.Write(b)
	return err
}

// SendStatus sends an HTTP status with empty body.
func (c *Context) SendStatus(code int) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	c.Writer.Header().Set(HeaderContentType, ContentTypeTextUTF8)
	c.Writer.Header().Set(HeaderXContentTypeOptions, "nosniff")
	c.markResponseCommitted()
	c.Writer.WriteHeader(code)
	return nil
}

// PushStream writes a streaming octet response with manual flush.
func (c *Context) PushStream(fn func(w io.Writer, flush func())) error {
	if c.ResponseCommitted() {
		return ErrResponseCommitted
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		return http.ErrNotSupported
	}
	c.Writer.Header().Set(HeaderContentType, ContentTypeOctetStream)
	c.Writer.Header().Set(HeaderXContentTypeOptions, "nosniff")
	c.markResponseCommitted()
	c.Writer.WriteHeader(http.StatusOK)
	flush := func() {
		flusher.Flush()
	}
	fn(c.Writer, flush)
	return nil
}

// PushSSE sets up Server-Sent Events stream.
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
	c.Writer.Header().Set(HeaderXContentTypeOptions, "nosniff")
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

// Accepts returns the preferred media type according to the Accept header.
func (c *Context) Accepts(candidates ...string) string {
	if len(candidates) == 0 {
		return ""
	}
	accept := c.GetHeader(HeaderAccept)
	if strings.TrimSpace(accept) == "" {
		return candidates[0]
	}
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

	for _, p := range prefs {
		for _, cand := range candidates {
			if matchesMedia(p.value, cand) {
				return cand
			}
		}
	}
	return candidates[0]
}

type acceptSpec struct {
	value string
	q     float64
	i     int
}

func parseAcceptSpec(s string) acceptSpec {
	as := acceptSpec{value: s, q: 1.0}
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
	av := strings.TrimSpace(strings.ToLower(acceptVal))
	cv := strings.TrimSpace(strings.ToLower(candidate))
	if av == "*/*" || av == cv {
		return true
	}
	if strings.HasSuffix(av, "/*") {
		return strings.HasPrefix(cv, strings.TrimSuffix(av, "*"))
	}
	return false
}

// Negotiate writes response based on the request's Accept header.
func (c *Context) Negotiate(code int, candidates map[string]any) {
	if len(candidates) == 0 {
		_ = c.String(code, "")
		return
	}
	keys := make([]string, 0, len(candidates))
	for k := range candidates {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	ct := c.Accepts(keys...)
	payload := candidates[ct]

	switch ct {
	case ContentTypeJSON, ContentTypeProblemJSON:
		_ = c.JSON(code, payload)
	case "text/plain":
		if s, ok := payload.(string); ok {
			_ = c.String(code, "%s", s)
		} else {
			_ = c.String(code, "")
		}
	case "text/html":
		if s, ok := payload.(string); ok {
			_ = c.HTML(code, s)
		} else {
			_ = c.HTML(code, "")
		}
	case "application/xml", "text/xml":
		_ = c.XML(code, payload)
	default:
		if v, ok := candidates[ContentTypeJSON]; ok {
			_ = c.JSON(code, v)
			return
		}
		first := keys[0]
		if s, ok := candidates[first].(string); ok {
			_ = c.String(code, "%s", s)
			return
		}
		_ = c.JSON(code, candidates[first])
	}
}

// Problem is a serializable RFC 9457 error object.
type Problem struct {
	Type     string         `json:"type,omitempty"`
	Title    string         `json:"title,omitempty"`
	Status   int            `json:"status,omitempty"`
	Detail   string         `json:"detail,omitempty"`
	Instance string         `json:"instance,omitempty"`
	Ext      map[string]any `json:"-"`
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
		if _, exists := base[k]; !exists {
			base[k] = v
		}
	}
	return json.Marshal(base)
}

// Problem writes an application/problem+json response using RFC 9457 format.
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
	c.Writer.Header().Set(HeaderContentType, ContentTypeProblemJSONUTF8)
	c.Writer.Header().Set(HeaderXContentTypeOptions, "nosniff")
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
