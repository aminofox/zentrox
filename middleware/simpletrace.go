package middleware

import (
	"net/http"
	"time"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/telemetry"
)

// SimpleTraceConfig controls basic tracing.
type SimpleTraceConfig struct {
	HeaderName    string // propagation header, default "traceparent"
	TraceKey      string // context key for trace id, default "trace_id"
	SpanKey       string // context key for span id, default "span_id"
	WithUserAgent bool
	WithHost      bool
}

func DefaultSimpleTrace() SimpleTraceConfig {
	return SimpleTraceConfig{
		HeaderName:    zentrox.TraceParent,
		TraceKey:      zentrox.TraceID,
		SpanKey:       zentrox.SpanID,
		WithUserAgent: true,
		WithHost:      true,
	}
}

func SimpleTrace(cfg SimpleTraceConfig) zentrox.Handler {
	if cfg.HeaderName == "" {
		cfg.HeaderName = zentrox.TraceParent
	}
	if cfg.TraceKey == "" {
		cfg.TraceKey = zentrox.TraceID
	}
	if cfg.SpanKey == "" {
		cfg.SpanKey = zentrox.SpanID
	}

	return func(c *zentrox.Context) {
		// Extract incoming trace if present.
		var traceID, parentSpanID string
		if tp := c.GetHeader(cfg.HeaderName); tp != "" {
			if t, p, ok := telemetry.ParseTraceParent(tp); ok {
				traceID, parentSpanID = t, p
			}
		}
		if traceID == "" {
			traceID = telemetry.NewTraceID()
		}
		spanID := telemetry.NewSpanID()

		// Store in context and propagate on response.
		c.Set(cfg.TraceKey, traceID)
		c.Set(cfg.SpanKey, spanID)
		c.SetHeader(cfg.HeaderName, telemetry.MakeTraceParent(traceID, spanID))

		// Capture status/bytes.
		sw := &statusWriter{ResponseWriter: c.Writer}
		c.Writer = sw

		start := time.Now()
		c.Forward()
		if sw.status == 0 {
			sw.status = http.StatusOK
		}

		attrs := map[string]string{
			"http.method": c.Request.Method,
			"http.target": c.Request.URL.Path,
			"net.peer":    c.Request.RemoteAddr,
		}
		if cfg.WithUserAgent {
			attrs["http.user_agent"] = c.Request.UserAgent()
		}
		if cfg.WithHost {
			attrs["http.host"] = c.Request.Host
		}

		telemetry.NewServerSpan(
			start,
			time.Now(),
			c.Request.Method+" "+c.Request.URL.Path, traceID, parentSpanID, spanID,
			attrs,
			sw.status,
		)
	}
}
