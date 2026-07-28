# Zentrox
![Zentrox](./zentrox.png)

A minimal, fast HTTP framework for Go with simple, clean API.

---

## Quick Start

```go
package main

import (
    "github.com/aminofox/zentrox/v2"
    "github.com/aminofox/zentrox/v2/middleware"
)

func main() {
    app := zentrox.NewApp()

    app.Plug(middleware.Recovery(), middleware.Logger())

    app.GET("/", func(c *zentrox.Context) {
        c.String(200, "Hello!")
    })

    app.GET("/users/:id", func(c *zentrox.Context) {
        c.JSON(200, map[string]string{"id": c.Param("id")})
    })

    app.Run(":8000")
}
```

---

## Installation

```bash
go get github.com/aminofox/zentrox/v2
```

---

## Features

- ✅ **Minimal & Fast** - Only essential middleware included
- ✅ **Simple API** - Clean and easy to learn
- ✅ **Easy Integration** - Custom logger and JWT support for your existing systems
- ✅ **Automatic middleware chaining** - No manual `c.Next()` needed in handlers
- ✅ **Fast routing** - Compiled trie with path params and wildcards
- ✅ **Built-in essentials** - CORS, JWT, Gzip, logging, error handling
- ✅ **HTTP hardening middleware** - Security headers, request limits, method/URI guards
- ✅ **Validation & binding** - Built-in request validation
- ✅ **Context pooling** - Zero allocations on router/context hot paths

---

## Routing

```go
app.GET("/path", handler)
app.POST("/path", handler)
app.PUT("/path", handler)
app.PATCH("/path", handler)
app.DELETE("/path", handler)
```

### Path Parameters

```go
app.GET("/users/:id", func(c *zentrox.Context) {
    id := c.Param("id")
    c.JSON(200, map[string]string{"id": id})
})
```

### Wildcards

```go
app.GET("/files/*filepath", func(c *zentrox.Context) {
    path := c.Param("filepath")
    c.String(200, "File path: %s", path)
})
```

### Custom Methods and Slash Behavior

```go
app.Handle("REPORT", "/reports/:id", handler)
app.OPTIONS("/reports/:id", optionsHandler)
```

By default Zentrox keeps the historical `SlashNormalize` behavior, where repeated slashes are treated like one slash by the router. Security-sensitive apps can opt into stricter canonical paths:

```go
app.SetSlashBehavior(zentrox.SlashStrict)        // 404 on repeated/trailing slash
app.SetSlashBehavior(zentrox.SlashRedirectClean) // 308 redirect to the clean path
```

### Route Groups

```go
api := app.Scope("/api")
api.GET("/users", listUsers)
api.POST("/users", createUser)
```

Routes, middleware, scopes, lifecycle hooks and trusted proxy settings are frozen after the app starts serving. Register everything during startup.

---

## Middleware

### Global Middleware

```go
app.Plug(
    middleware.Recovery(),
    middleware.Logger(),
    middleware.CORS(middleware.DefaultCORS()),
)
```

### Per-Route Middleware

```go
app.GET("/secure", authMiddleware, handler)
```

### Group Middleware

```go
admin := app.Scope("/admin", authMiddleware)
admin.GET("/stats", statsHandler)

// Or add middleware after creating the group
apiGroup := app.Scope("/api")
apiGroup.Use(authMiddleware)
apiGroup.GET("/users", listUsers)
```

### Custom Middleware

```go
func MyMiddleware() zentrox.Handler {
    return func(c *zentrox.Context) {
        // Before handler
        c.Next() // Call next middleware/handler
        // After handler
    }
}
```

`c.Next()` advances the chain once; calling it again after the rest of the stack has run is a no-op. Call `c.Abort()` after writing a terminal response to stop later handlers.

### Built-in Middleware

Zentrox includes essential middleware plus lightweight utilities:

```go
middleware.Recovery()                           // Panic recovery
middleware.Logger()                             // Request logging
middleware.LoggerWithFunc(customLogFn)          // Custom logger integration
middleware.CORS(middleware.DefaultCORS())       // CORS headers
middleware.Gzip()                               // Response compression
middleware.JWT(middleware.JWTConfig{Secret: secret}) // JWT auth
middleware.ErrorHandler(middleware.DefaultErrorHandler()) // Error handling
middleware.RequestID(middleware.DefaultRequestID()) // Request ID propagation
middleware.RateLimit(middleware.DefaultRateLimit()) // Token-bucket rate limit
middleware.Timeout(2 * time.Second)             // Request context timeout
middleware.SecurityHeaders(middleware.DefaultSecurityHeaders()) // Baseline security headers
middleware.HTTPProtection(middleware.DefaultHTTPProtection()) // Method + URI guards
middleware.BodyLimit(middleware.DefaultBodyLimit()) // Request body size limit
middleware.ConcurrencyLimit(middleware.DefaultConcurrencyLimit()) // In-flight request cap
middleware.DefaultAPIHardening()... // Preset stack (use with app.Plug)
middleware.DefaultAPIHardeningFast()... // Lower-overhead preset
```

## CORS (Simplified)

```go
app.Plug(middleware.CORS(middleware.CORSConfig{
    AllowOrigins:     []string{"http://localhost:3000"},
    AllowMethods:     []string{"GET", "POST", "PUT", "DELETE"},
    AllowHeaders:     []string{"Content-Type", "Authorization"},
    AllowCredentials: true,
    MaxAge:           3600,
}))
```

Or use defaults:

```go
app.Plug(middleware.CORS(middleware.DefaultCORS()))
```

Credentialed CORS requires explicit origins or `AllowOriginFunc`; wildcard origins are only emitted when credentials are disabled.

---

## JWT (Simplified)

One simple config - no more separate JWT and JWTChecks:

```go
import (
    "os"
    "time"
)

secret := []byte(os.Getenv("JWT_SECRET")) // use a strong secret from config/secrets storage

app.Plug(middleware.JWT(middleware.JWTConfig{
	Secret:     secret,
	Issuer:     "zentrox-app",
	Audience:   "api",
	ClockSkew:  30 * time.Second,
	ContextKey: "user",
	ValidateFunc: func(claims *middleware.JWTClaims) error {
		// Optional domain checks, for example account status or token revocation.
		return nil
	},
}))
```

Get user in handler:

```go
app.GET("/me", func(c *zentrox.Context) {
    claims, _ := c.Get("user")
    c.JSON(200, claims)
})
```

The built-in JWT middleware supports HS256 only. It validates `exp`, `nbf`, future `iat`, issuer and audience when configured, rejects empty secrets, and rejects multiple Authorization headers. For OIDC/JWKS/key rotation, plug in a dedicated auth package with `WrapHTTP` or custom middleware.

## Request ID

```go
app.Plug(middleware.RequestID(middleware.DefaultRequestID()))

app.GET("/trace", func(c *zentrox.Context) {
    c.JSON(200, map[string]any{"request_id": c.RequestID()})
})
```

## Rate Limit

```go
app.Plug(middleware.RateLimit(middleware.RateLimitConfig{
    Rate:  20, // requests/sec
    Burst: 40,
    KeyFunc: func(c *zentrox.Context) string {
        return c.RealIP()
    },
}))
```

## Timeout

```go
app.Plug(middleware.Timeout(2 * time.Second))

app.GET("/slow", func(c *zentrox.Context) {
    select {
    case <-time.After(3 * time.Second):
        c.String(200, "done")
    case <-c.Done():
        return
    }
})
```

---

## Security Headers

```go
app.Plug(middleware.SecurityHeaders(middleware.DefaultSecurityHeaders()))
```

Custom config:

```go
app.Plug(middleware.SecurityHeaders(middleware.SecurityHeadersConfig{
    XContentTypeOptions: "nosniff",
    XFrameOptions:       "SAMEORIGIN",
    ReferrerPolicy:      "strict-origin",
    Extra: map[string]string{
        "Permissions-Policy": "geolocation=()",
    },
}))
```

## HTTP Protection

```go
app.Plug(middleware.HTTPProtection(middleware.HTTPProtectionConfig{
    AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"},
    MaxURLLength:   2048,
}))
```

## Body Limit

```go
app.Plug(middleware.BodyLimit(middleware.BodyLimitConfig{
    MaxBytes: 1 << 20, // 1 MiB
}))
```

## Concurrency Limit

```go
app.Plug(middleware.ConcurrencyLimit(middleware.ConcurrencyLimitConfig{
    MaxConcurrent: 512,
    QueueTimeout:  50 * time.Millisecond,
}))
```

Set `QueueTimeout: 0` to reject immediately when all slots are busy.

## Default API Hardening (Preset)

Use the optimized preset directly:

```go
app.Plug(middleware.DefaultAPIHardening()...)
```

Or tune defaults:

```go
cfg := middleware.DefaultAPIHardeningConfig()
cfg.BodyLimit.MaxBytes = 2 << 20 // 2 MiB
cfg.ConcurrencyLimit.MaxConcurrent = 1024
cfg.Timeout = 1500 * time.Millisecond

app.Plug(middleware.APIHardening(cfg)...)
```

## Default API Hardening Fast (Preset)

Use this when you want lower middleware overhead and can skip request-id/timeout:

```go
app.Plug(middleware.DefaultAPIHardeningFast()...)
```

Tune fast preset:

```go
cfg := middleware.DefaultAPIHardeningFastConfig()
cfg.ConcurrencyLimit.MaxConcurrent = 1536
cfg.RateLimit.Rate = 50
cfg.RateLimit.Burst = 100

app.Plug(middleware.APIHardeningFast(cfg)...)
```

For API services, a practical default stack is: `RequestID + SecurityHeaders + HTTPProtection + BodyLimit + ConcurrencyLimit + RateLimit + Timeout`.

Performance note: these hardening middleware precompute config and keep per-request work lightweight. You can measure impact with:

```bash
go test ./z_test -bench BenchmarkRPS_ -benchmem
go test ./z_test -run '^$' -bench BenchmarkMiddlewareCost_ -benchmem
```

---

## Static Files & Uploads

Static file serving cleans request paths, rejects traversal outside the mounted directory, and blocks symlink targets that resolve outside `Dir` by default:

```go
app.Static("/assets", zentrox.StaticOptions{
    Dir:        "./public",
    Index:      "index.html",
    AllowedExt: []string{".html", ".css", ".js", ".png", ".jpg", ".svg", ".ico"},
})
```

Set `FollowSymlinks: true` only when the mounted directory and all symlink targets are trusted.

For uploads, prefer a dedicated destination directory, extension allow-list, filename sanitization and generated names:

```go
saved, err := c.SaveUploadedFile("file", "./uploads", zentrox.UploadOptions{
    MaxMemory:          10 << 20,
    AllowedExt:         []string{".png", ".jpg", ".jpeg", ".pdf"},
    Sanitize:           true,
    GenerateUniqueName: true,
    Overwrite:          false,
})
```

`SaveUploadedFile` always saves under the destination directory, refuses symlink overwrite, and creates files with private `0600` permissions.

---

## Binding & Validation

```go
type CreateUser struct {
    Name  string `json:"name" validate:"required,min=3,max=50"`
    Email string `json:"email" validate:"required,email"`
    Age   int    `json:"age" validate:"min=18,max=130"`
}

app.POST("/users", func(c *zentrox.Context) {
    var input CreateUser
    if err := c.BindJSONInto(&input); err != nil {
        c.Fail(400, "invalid input", err.Error())
        return
    }
    c.JSON(201, input)
})
```

Use `BindStrictJSONInto` for security-sensitive endpoints. It rejects unknown fields, duplicate JSON object keys and multiple JSON documents.

Supported validators:
- `required` - field must be present
- `min=N`, `max=N` - min/max value or length
- `len=N` - exact length
- `email` - valid email
- `oneof=a b c` - value must be one of
- `regex=pattern` - match regex

The built-in validator is intentionally small. For larger apps, plug in a dedicated validator during startup:

```go
import playground "github.com/go-playground/validator/v10"

validate := playground.New()
app.SetValidatorFunc(validate.Struct)
```

Any custom engine can be used by implementing:

```go
type StructValidator interface {
    ValidateStruct(v any) error
}
```

---

## Extension Points

Zentrox keeps the core small and lets apps replace heavier subsystems during startup.

Use a dedicated validator package when the built-in tags are not enough:

```go
validate := playground.New()
app.SetValidatorFunc(validate.Struct)
```

Use a custom JSON codec when you want another serializer:

```go
app.SetJSONCodecFuncs(json.Marshal, json.Unmarshal)
```

Both hooks are frozen once the app starts serving, so request behavior stays stable at runtime.

---

For more examples, see `examples/` (including `examples/platform_middleware/`).

## Examples Matrix

- `examples/minimal/` - Minimal setup: custom logger, JWT sign + protected route with claim validation
- `examples/basic/` - Core routing, lifecycle hooks, static files, file upload
- `examples/binding/` - JSON/form/query binding + validation
- `examples/graceful/` - `Start` + graceful `Shutdown` with signals and health endpoints
- `examples/platform_middleware/` - `DefaultAPIHardening` preset with tuned RateLimit/Timeout

---

## Context API

```go
// Input
c.Param("id")           // Path parameter
c.Query("q")            // Query parameter
c.GetHeader("X-Token")  // Request header

// Binding
c.BindJSONInto(&dst)    // Bind & validate JSON
c.BindStrictJSONInto(&dst) // Strict JSON: unknown/duplicate keys rejected
c.BindFormInto(&dst)    // Bind & validate form
c.BindQueryInto(&dst)   // Bind & validate query

// Output
c.JSON(200, data)       // Send JSON, returns write/encode errors
c.String(200, "ok")     // Send text, returns write errors
c.HTML(200, html)       // Send HTML, returns write errors
c.XML(200, data)        // Send XML, returns marshal/write errors
c.Data(200, "text/plain", bytes)  // Send raw bytes, returns write errors
c.SendStatus(200)       // Send status only, returns write errors
c.SetHeader("X-ID", id) // Response header

// Storage
c.Set("key", value)     // Store value
c.Get("key")            // Retrieve value
c.Copy()                // Shallow request-safe copy for background goroutines
```

Do not keep `*zentrox.Context` after the request returns. Use `c.Copy()` and avoid writing to `ResponseWriter` from background goroutines.

---

## Performance

Zentrox is designed for a small, fast routing and middleware path:
- Context pooling (zero allocations on router/context hot paths)
- Fast routing (compiled trie)
- Efficient middleware chain

Benchmark numbers depend on Go version, CPU, concurrency, transport, payload and middleware stack. Reproduce local numbers with:

```bash
go test ./z_test -bench . -benchmem
go test -race ./...
```

---

## Complete Example

```go
package main

import (
    "os"
    "time"

    "github.com/aminofox/zentrox/v2"
    "github.com/aminofox/zentrox/v2/middleware"
)

type User struct {
    ID    string `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email" validate:"required,email"`
}

func main() {
    app := zentrox.NewApp()

    // Global middleware
    app.Plug(
        middleware.CORS(middleware.DefaultCORS()),
        middleware.Recovery(),
        middleware.Logger(),
    )

    // Public routes
    app.GET("/", func(c *zentrox.Context) {
        c.String(200, "Welcome to Zentrox!")
    })

    app.GET("/ping", func(c *zentrox.Context) {
        c.JSON(200, map[string]string{"status": "ok"})
    })

    // API routes
    api := app.Scope("/api")
    
    api.GET("/users/:id", func(c *zentrox.Context) {
        user := User{
            ID:    c.Param("id"),
            Name:  "John Doe",
            Email: "john@example.com",
        }
        c.JSON(200, user)
    })

    api.POST("/users", func(c *zentrox.Context) {
        var user User
        if err := c.BindJSONInto(&user); err != nil {
            c.Fail(400, "invalid input", err.Error())
            return
        }
        user.ID = "generated-id"
        c.JSON(201, user)
    })

    // Protected routes
    secret := []byte(os.Getenv("JWT_SECRET"))
    admin := app.Scope("/admin", middleware.JWT(middleware.JWTConfig{
        Secret: secret,
    }))

    admin.GET("/stats", func(c *zentrox.Context) {
        c.JSON(200, map[string]int{
            "users":  100,
            "orders": 50,
        })
    })

    app.Run(":8000")
}
```

---

## Why Zentrox?

- **Minimal by Design**: Focused middleware set - no bloat, easy to understand
- **Clean API**: Less boilerplate, cleaner patterns, better defaults
- **Easy Integration**: Custom logger and JWT support to fit your existing systems
- **Faster routing**: Compiled trie-based router with reproducible local benchmarks
- **Better defaults**: Security and performance out of the box
- **Modern features**: Validation, HTTP hardening middleware, automatic middleware chaining
- **Production-minded core**: Context pooling, panic recovery, route freezing and safer response helpers

---

## License

MIT
