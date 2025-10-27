# Zentrox
![Zentrox](./zentrox.png)

A minimal, fast HTTP framework for Go with simple, clean API.

---

## Quick Start

```go
package main

import (
    "github.com/aminofox/zentrox"
    "github.com/aminofox/zentrox/middleware"
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
go get github.com/aminofox/zentrox
```

---

## Features

- ✅ **Minimal & Fast** - Only essential middleware included
- ✅ **Simple API** - Clean and easy to learn
- ✅ **Easy Integration** - Custom logger and JWT support for your existing systems
- ✅ **Automatic middleware chaining** - No manual `c.Next()` needed in handlers
- ✅ **Fast routing** - Compiled trie with path params and wildcards
- ✅ **Built-in essentials** - CORS, JWT, Gzip, logging, error handling
- ✅ **Swagger support** - Use swaggo with comment annotations for API documentation
- ✅ **Validation & binding** - Built-in request validation
- ✅ **Context pooling** - Zero allocations for high performance

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

### Route Groups

```go
api := app.Scope("/api")
api.GET("/users", listUsers)
api.POST("/users", createUser)
```

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

### Built-in Middleware

Zentrox includes only essential middleware for a minimal footprint:

```go
middleware.Recovery()                           // Panic recovery
middleware.Logger()                             // Request logging
middleware.LoggerWithFunc(customLogFn)          // Custom logger integration
middleware.CORS(middleware.DefaultCORS())       // CORS headers
middleware.Gzip()                               // Response compression
middleware.JWT(middleware.DefaultJWT(secret))   // JWT auth
middleware.ErrorHandler(middleware.DefaultErrorHandler()) // Error handling
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

---

## JWT (Simplified)

One simple config - no more separate JWT and JWTChecks:

```go
secret := []byte("your-secret-key")

app.Plug(middleware.JWT(middleware.JWTConfig{
    Secret:      secret,
    ContextKey:  "user",
    RequireExp:  true,
    Issuer:      "your-app",
    Audience:    "api",
    ClockSkew:   60 * time.Second,
    AllowedAlgs: []string{"HS256"},
}))
```

Or use defaults:

```go
app.Plug(middleware.JWT(middleware.DefaultJWT(secret)))
```

Get user in handler:

```go
app.GET("/me", func(c *zentrox.Context) {
    user, _ := c.Get("user")
    c.JSON(200, user)
})
```

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

Supported validators:
- `required` - field must be present
- `min=N`, `max=N` - min/max value or length
- `len=N` - exact length
- `email` - valid email
- `oneof=a b c` - value must be one of
- `regex=pattern` - match regex

---

## Swagger/OpenAPI

Zentrox uses [swaggo](https://github.com/swaggo/swag) for API documentation with comment annotations:

### 1. Install swag CLI

```bash
go install github.com/swaggo/swag/cmd/swag@latest
```

### 2. Add annotations to your code

```go
package main

import (
    "github.com/aminofox/zentrox"
    _ "yourapp/docs" // Import generated docs
)

// @title           My API
// @version         1.0
// @description     This is my API server
// @host            localhost:8000
// @BasePath        /api/v1

func main() {
    app := zentrox.NewApp()
    
    // Mount Swagger UI
    app.ServeSwagger("/swagger")
    
    // @Summary      Get user by ID
    // @Tags         users
    // @Param        id   path      int  true  "User ID"
    // @Success      200  {object}  User
    // @Router       /users/{id} [get]
    app.GET("/api/v1/users/:id", getUser)
    
    app.Run(":8000")
}
```

### 3. Generate documentation

```bash
swag init
```

### 4. Access Swagger UI

Visit `http://localhost:8000/swagger/index.html`

For more examples, see `examples/swagger_annotations/` directory.

---

## Context API

```go
// Input
c.Param("id")           // Path parameter
c.Query("q")            // Query parameter
c.GetHeader("X-Token")  // Request header

// Binding
c.BindJSONInto(&dst)    // Bind & validate JSON
c.BindFormInto(&dst)    // Bind & validate form
c.BindQueryInto(&dst)   // Bind & validate query

// Output
c.JSON(200, data)       // Send JSON
c.String(200, "ok")     // Send text (with format support)
c.HTML(200, html)       // Send HTML
c.XML(200, data)        // Send XML
c.Data(200, "text/plain", bytes)  // Send raw bytes
c.SendStatus(200)       // Send status only
c.SetHeader("X-ID", id) // Response header

// Storage
c.Set("key", value)     // Store value
c.Get("key")            // Retrieve value
```

---

## Performance

Zentrox is designed for speed:
- Context pooling (zero allocations per request)
- Fast routing (compiled trie)
- Efficient middleware chain

Benchmarks on Apple M1 Pro:
- ~1M rps for static routes
- ~900K rps for parameterized routes
- ~740K rps for JSON responses

---

## Complete Example

```go
package main

import (
    "github.com/aminofox/zentrox"
    "github.com/aminofox/zentrox/middleware"
    "time"
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
    secret := []byte("your-secret-key")
    admin := app.Scope("/admin", middleware.JWT(middleware.JWTConfig{
        Secret:     secret,
        RequireExp: true,
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

- **Minimal by Design**: Only 6 essential middleware - no bloat, easy to understand
- **Clean API**: Less boilerplate, cleaner patterns, better defaults
- **Easy Integration**: Custom logger and JWT support to fit your existing systems
- **Faster routing**: Compiled trie-based router with ~2M rps
- **Better defaults**: Security and performance out of the box
- **Modern features**: Built-in Swagger (via swaggo), validation, automatic middleware chaining
- **Production-ready**: Context pooling, panic recovery, zero allocations

---

## License

MIT
