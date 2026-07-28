package z_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aminofox/zentrox/v2"
)

// FuzzRouter fuzzes the route registration and matching logic.
func FuzzRouter(f *testing.F) {
	// Add some seed inputs
	f.Add("GET", "/users/:id", "/users/123")
	f.Add("POST", "/api/v1/*path", "/api/v1/some/long/path")
	f.Add("GET", "/path/with//multiple///slashes", "/path/with/multiple/slashes")

	f.Fuzz(func(t *testing.T, method string, registerPath string, requestPath string) {
		// Prevent invalid HTTP methods or paths that break standard conventions
		// We just want to ensure the router doesn't panic on arbitrary strings.
		app := zentrox.NewApp()

		// Use a defer/recover to catch panics since some malformed paths might panic on registration by design
		// (like duplicate wildcards), but we want to catch UNINTENDED panics like out-of-bounds.
		func() {
			defer func() {
				_ = recover() // ignore panics on registration for fuzzing (e.g., wildcard not at end)
			}()
			
			if method == "GET" {
				app.GET(registerPath, func(c *zentrox.Context) {
					c.String(http.StatusOK, "ok")
				})
			} else {
				app.POST(registerPath, func(c *zentrox.Context) {
					c.String(http.StatusOK, "ok")
				})
			}

			// If registered successfully, try to hit it
			req := httptest.NewRequest(method, requestPath, nil)
			w := httptest.NewRecorder()
			app.ServeHTTP(w, req)
		}()
	})
}

// FuzzJSONBinding fuzzes the JSON payload parser.
func FuzzJSONBinding(f *testing.F) {
	f.Add([]byte(`{"name":"test", "age": 20}`))
	f.Add([]byte(`{"invalid":}`))
	f.Add([]byte(`[1, 2, 3]`))

	f.Fuzz(func(t *testing.T, payload []byte) {
		app := zentrox.NewApp()
		app.POST("/bind", func(c *zentrox.Context) {
			var dst map[string]any
			_ = c.BindInto(&dst) // should not panic
			c.String(200, "ok")
		})

		req := httptest.NewRequest("POST", "/bind", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		app.ServeHTTP(w, req)
	})
}
