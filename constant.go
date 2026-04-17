package zentrox

const (
	AppVersion  = "app_version"
	RequestID   = "request_id"
	XRequestID  = "X-Request-ID"
	TraceParent = "traceparent"
	TraceID     = "trace_id"
	SpanID      = "span_id"
)

const (
	HeaderAccept              = "Accept"
	HeaderAllow               = "Allow"
	HeaderAuthorization       = "Authorization"
	HeaderOrigin              = "Origin"
	HeaderConnection          = "Connection"
	HeaderContentType         = "Content-Type"
	HeaderContentLength       = "Content-Length"
	HeaderContentEncoding     = "Content-Encoding"
	HeaderContentDisposition  = "Content-Disposition"
	HeaderETag                = "ETag"
	HeaderCacheControl        = "Cache-Control"
	HeaderLastModified        = "Last-Modified"
	HeaderIfNoneMatch         = "If-None-Match"
	HeaderIfModifiedSince     = "If-Modified-Since"
	HeaderAcceptEncoding      = "Accept-Encoding"
	HeaderXForwardedFor       = "X-Forwarded-For"
	HeaderXRealIP             = "X-Real-IP"
	HeaderXContentTypeOptions = "X-Content-Type-Options"
	HeaderXFrameOptions       = "X-Frame-Options"
	HeaderReferrerPolicy      = "Referrer-Policy"
)

const (
	HeaderAccessControlAllowOrigin      = "Access-Control-Allow-Origin"
	HeaderAccessControlAllowMethods     = "Access-Control-Allow-Methods"
	HeaderAccessControlAllowHeaders     = "Access-Control-Allow-Headers"
	HeaderAccessControlExposeHeaders    = "Access-Control-Expose-Headers"
	HeaderAccessControlAllowCredentials = "Access-Control-Allow-Credentials"
	HeaderAccessControlMaxAge           = "Access-Control-Max-Age"
	HeaderVary                          = "Vary"
)

const (
	ContentTypeJSONUTF8        = "application/json; charset=utf-8"
	ContentTypeTextUTF8        = "text/plain; charset=utf-8"
	ContentTypeHTMLUTF8        = "text/html; charset=utf-8"
	ContentTypeXMLUTF8         = "application/xml; charset=utf-8"
	ContentTypeOctetStream     = "application/octet-stream"
	ContentTypeEventStream     = "text/event-stream"
	ContentTypeProblemJSON     = "application/problem+json"
	ContentTypeProblemJSONUTF8 = "application/problem+json; charset=utf-8"
	ContentTypeFormURLEncoded  = "application/x-www-form-urlencoded"
	ContentTypeMultipartForm   = "multipart/form-data"
	ContentTypeJSON            = "application/json"
)

const (
	BearerPrefix = "Bearer "
)

const (
	CacheControlNoCache = "no-cache"
	ConnectionKeepAlive = "keep-alive"
)

const (
	MsgInternalServerError = "internal server error"
	MsgMissingToken        = "missing token"
	MsgInvalidToken        = "invalid token"
	MsgUnsupportedAlg      = "unsupported algorithm"
	MsgInvalidSignature    = "invalid signature"
	MsgTooManyRequests     = "too many requests"
	MsgRequestTimeout      = "request timeout"
	MsgNotFound            = "not found"
	MsgForbidden           = "forbidden"
	MsgMethodNotAllowed    = "method not allowed"
	MsgURITooLong          = "uri too long"
	MsgPayloadTooLarge     = "payload too large"
	MsgServerBusy          = "server busy"
	MsgStatError           = "stat error"
	MsgOpenError           = "open error"
	MsgFileNotFound        = "file not found"
	MsgJSONEncodeFailed    = "json encode failed"
)
