# Zentrox Simplification Changes

This document summarizes the major simplifications made to make Zentrox easier to integrate into other projects.

## Summary

Zentrox has been streamlined to be minimal, easy to understand, and simple to integrate with existing projects. The focus is on providing essential functionality while allowing developers to customize and extend as needed.

## Major Changes

### 1. Middleware System Simplified

**Before:** Middleware required calling `c.Forward()` to pass control
```go
func MyMiddleware() zentrox.Handler {
    return func(c *zentrox.Context) {
        // do something
        c.Forward() // Required!
    }
}
```

**After:** Middleware uses `c.Next()` (automatically called if not explicitly invoked)
```go
func MyMiddleware() zentrox.Handler {
    return func(c *zentrox.Context) {
        // do something
        c.Next() // Optional in many cases
    }
}
```

### 2. JWT Middleware Simplified

**Before:** Complex configuration with many fields
```go
middleware.JWT(middleware.JWTConfig{
    Secret:      secret,
    RequireExp:  true,
    RequireNbf:  false,
    Issuer:      "https://example.com",
    Audience:    "api://myapp",
    ClockSkew:   60 * time.Second,
    AllowedAlgs: []string{"HS256"},
})
```

**After:** Simple config with custom validation callback
```go
middleware.JWT(middleware.JWTConfig{
    Secret: secret,
    ValidateFunc: func(claims map[string]any) error {
        // Custom validation logic
        if exp, ok := claims["exp"].(float64); ok {
            if time.Now().Unix() > int64(exp) {
                return errors.New("token expired")
            }
        }
        return nil
    },
})
```

### 3. CORS Simplified

**Before:** Two separate middlewares (CORS and StrictCORS)
```go
app.Plug(middleware.CORS(...))
app.Plug(middleware.StrictCORS(...)) // separate middleware
```

**After:** Single CORS middleware
```go
app.Plug(middleware.CORS(middleware.CORSConfig{
    AllowOrigins: []string{"*"},
    AllowMethods: []string{"GET", "POST", "PUT", "DELETE"},
    AllowHeaders: []string{"*"},
}))
```

### 4. Logger Supports Custom Integration

**Before:** Only built-in logger
```go
app.Plug(middleware.Logger())
```

**After:** Easy custom logger integration
```go
// Use your existing logger (zap, logrus, etc.)
app.Plug(middleware.LoggerWithFunc(func(method, path string, status int, dur time.Duration, err error) {
    myLogger.Info("request", "method", method, "path", path, "status", status)
}))
```

### 5. Removed Non-Essential Middleware

The following middleware was removed to keep the framework minimal:
- **AccessLog** - Use LoggerWithFunc instead
- **Metrics** - Use your own metrics library
- **SimpleTrace** - Use your own tracing solution
- **Timeout** - Implement at handler level if needed
- **RequestID** - Implement as custom middleware if needed
- **Helpers** - Functionality moved to core where needed

### 6. Examples Over Documentation

All features are demonstrated in the `examples/` directory with practical, working code:
- `minimal/` - Basic setup with custom logger
- `jwt_custom/` - Custom JWT validation
- `basic/` - Simple routing
- `binding/` - Request binding
- `gzip/` - Compression
- `graceful/` - Graceful shutdown
- And more...

## Kept Middleware (Essential)

These middleware remain because they provide core functionality:
- **Logger** - Request logging with custom function support
- **Recovery** - Panic recovery
- **ErrorHandler** - Standardized error responses
- **JWT** - Token authentication
- **CORS** - Cross-origin requests
- **Gzip** - Response compression

## Migration Guide

### If you were using c.Forward()
Replace all `c.Forward()` calls with `c.Next()`

### If you were using complex JWT config
Replace with ValidateFunc:
```go
ValidateFunc: func(claims map[string]any) error {
    // Your validation logic here
    return nil
}
```

### If you were using deleted middleware
- AccessLog → Use `middleware.LoggerWithFunc(yourLogger)`
- Metrics → Use Prometheus or your metrics library directly
- RequestID → Create custom middleware if needed
- Timeout → Use `http.TimeoutHandler` or context timeout

## Benefits

1. **Smaller codebase** - Removed ~500 lines of middleware code
2. **Easier to learn** - Familiar patterns from popular frameworks
3. **Flexible** - Custom validators instead of rigid configs
4. **Integrates easily** - Works with existing logger/metrics/tracing
5. **Practical examples** - Learn from working code, not docs

## Philosophy

Zentrox now follows these principles:
- **Minimal core** - Essential features only
- **Easy integration** - Works with your existing tools
- **Examples first** - Show, don't tell
- **No magic** - Clear, understandable code
- **Extensible** - Easy to add custom middleware

## Testing

All changes are covered by tests:
```bash
go test ./...
```

All examples can be run:
```bash
go run examples/minimal/main.go
go run examples/jwt_custom/main.go
# etc.
```
