package zentrox

// HTTPError is the canonical error payload returned by the framework.
type HTTPError struct {
	Code     int    `json:"code"`
	Message  string `json:"message"`
	Detail   any    `json:"detail,omitempty"`
	Internal error  `json:"-"`
}

func (e *HTTPError) Error() string {
	if e.Internal != nil {
		return e.Message + ": " + e.Internal.Error()
	}
	return e.Message
}

// SetInternal attaches an internal error to the HTTPError (e.g., for logging) 
// without exposing it to the client in JSON serialization.
func (e *HTTPError) SetInternal(err error) *HTTPError {
	e.Internal = err
	return e
}

// NewHTTPError constructs a new HTTPError.
func NewHTTPError(code int, message string, detail ...any) *HTTPError {
	var d any
	if len(detail) > 0 {
		d = detail[0]
	}
	return &HTTPError{Code: code, Message: message, Detail: d}
}
