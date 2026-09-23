package zentrox

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
)

// headWriter suppresses response body writes while still allowing headers/status.
// It is used to implement automatic HEAD behavior by reusing GET handlers.
type headWriter struct {
	http.ResponseWriter
	wroteHeader bool
	status      int
}

func (w *headWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.status = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *headWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return len(b), nil
}

func (w *headWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *headWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijacker not supported")
	}
	return h.Hijack()
}

func (w *headWriter) Push(target string, opts *http.PushOptions) error {
	p, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return p.Push(target, opts)
}

func (w *headWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *headWriter) ReadFrom(r io.Reader) (n int64, err error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return io.Copy(io.Discard, r)
}

// respRecorder captures status code and bytes without changing behavior.
// It is used to feed onResponse hook with final status/latency.
type respRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *respRecorder) Status() int {
	return w.status
}

func (w *respRecorder) BytesWritten() int {
	return w.bytes
}

func (w *respRecorder) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *respRecorder) WriteHeader(code int) {
	if w.status != 0 {
		return
	}
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *respRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func (w *respRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *respRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijacker not supported")
	}
	return h.Hijack()
}

func (w *respRecorder) Push(target string, opts *http.PushOptions) error {
	p, ok := w.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}
	return p.Push(target, opts)
}

func (w *respRecorder) ReadFrom(r io.Reader) (n int64, err error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	rf, ok := w.ResponseWriter.(io.ReaderFrom)
	if ok {
		n, err = rf.ReadFrom(r)
	} else {
		n, err = io.Copy(w.ResponseWriter, r)
	}
	w.bytes += int(n)
	return n, err
}
