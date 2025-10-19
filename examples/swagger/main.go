package main

import (
	"log"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/openapi"
)

type User struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type CreateUser struct {
	Name  string `json:"name"  validate:"required,min=2"`
	Email string `json:"email" validate:"required,email"`
}

type AppError struct {
	Detail string `json:"detail"`
	Status int    `json:"status"`
	Title  string `json:"title"`
	Type   string `json:"type"`
}

func AuthGuard() zentrox.Handler {
	return func(c *zentrox.Context) {
		if c.GetHeader("X-Token") != "secret" {
			c.Problemf(401, "unauthorized", "missing or invalid token")
			c.Abort()
			return
		}
		c.Forward()
	}
}

func AfterAuthGuard() zentrox.Handler {
	return func(c *zentrox.Context) {
		log.Println("AfterAuthGuard")
		c.Forward()
	}
}

func main() {
	app := zentrox.NewApp()
	b := openapi.New(
		"Zentrox Example API",
		"1.0.0",
		openapi.WithServer("http://localhost:8000", "local"),
		openapi.WithDescription("Example demonstrating OpenAPI with helper wrappers"),
	)

	app.SetPrintRoutes(true).
		SetEnableOpenAPI(true).
		MountOpenAPI(b, "/openapi.json", "/docs").
		SetPrintRoutes(true)

	app.OnGetDoc(b, "/users/:id", func(c *zentrox.Context) {
		u := User{ID: c.Param("id"), Name: "Alice", Email: "alice@example.com"}
		c.SendJSON(200, u)
	}, openapi.Op().
		SetSummary("Get user by ID").
		SetTag("users").
		ResponseJSON(200, User{}, "OK").
		ResponseProblem(404, "User not found", AppError{}).
		ResponseProblem(500, "Internal server error", AppError{}),
	)

	app.OnPostDoc(b, "/users", func(c *zentrox.Context) {
		var in CreateUser
		if err := c.BindJSONInto(&in); err != nil {
			c.Problemf(400, "invalid payload", err.Error())
			return
		}
		u := User{ID: "123", Name: in.Name, Email: in.Email}
		c.SendJSON(201, u)
	}, openapi.Op().
		SetSummary("Create new user").
		SetTag("users").
		RequestJSON(CreateUser{}, true, "payload").
		ResponseJSON(201, &User{}, "Created").
		ResponseProblem(400, "Bad request", AppError{}),
	)

	app.OnGetDoc(b, "/healthz", func(c *zentrox.Context) {
		c.SendJSON(200, map[string]any{
			"status": "ok",
		})
	}, openapi.Op().
		SetSummary("Health check").
		SetTag("system").
		ResponseJSON(200, map[string]string{"status": "ok"}, "OK"),
	)

	v1 := app.Scope("/api/v1")
	v1.OnGetDoc(b, "/users/:id", func(c *zentrox.Context) {
		u := User{ID: c.Param("id"), Name: "Bob", Email: "bob@example.com"}
		c.SendJSON(200, u)
	}, openapi.Op().
		SetSummary("Get v1 user by ID").
		SetTag("users").
		ResponseJSON(200, User{}, "OK").
		ResponseProblem(404, "User not found", AppError{}),
	)
	v1.OnPostDoc(b, "/users", func(c *zentrox.Context) {
		var in CreateUser
		if err := c.BindJSONInto(&in); err != nil {
			c.Problemf(400, "invalid payload", err.Error())
			return
		}
		u := User{ID: "v1-999", Name: in.Name, Email: in.Email}
		c.SendJSON(201, u)
	}, openapi.Op().
		SetSummary("Create v1 user").
		SetTag("users").
		RequestJSON(CreateUser{}, true, "payload").
		ResponseJSON(201, &User{}, "Created"),
	)

	v1.OnGetDoc(b, "/healthz", func(c *zentrox.Context) {
		c.SendJSON(200, map[string]any{"status": "ok", "scope": "v1"})
	}, openapi.Op().
		SetSummary("Health check v1").
		SetTag("system").
		ResponseJSON(200, map[string]any{"status": "ok", "scope": "v1"}, "OK"),
	)

	admin := app.Scope("/admin", AuthGuard(), AfterAuthGuard())
	admin.OnGetDoc(b, "/stats", func(c *zentrox.Context) {
		c.SendJSON(200, map[string]any{
			"users":  42,
			"orders": 7,
			"scope":  "admin",
		})
	}, openapi.Op().
		SetSummary("Admin stats").
		SetTag("admin").
		ResponseJSON(200, map[string]any{"users": 0, "orders": 0, "scope": "admin"}, "OK"),
	)

	log.Println("listening on :8000")
	_ = app.Run(":8000")
}
