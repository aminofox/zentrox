package main

import (
	"fmt"
	"log"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
)

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

	// global middlewares
	app.Plug(
		middleware.ErrorHandler(middleware.DefaultErrorHandler()),
	)

	app.OnGet("/public", func(c *zentrox.Context) {
		c.SendText(200, "public ok")
	})

	app.OnGet("/secure", AuthGuard(), AfterAuthGuard(), (func(c *zentrox.Context) {
		c.SendText(200, "secure ok")
	}))

	api := app.Scope("api", AuthGuard())
	{
		api.OnGet("/users", func(ctx *zentrox.Context) {
			ctx.SendText(200, "list ok")
		})
		api.OnGet("/user/:id", AfterAuthGuard(), func(ctx *zentrox.Context) {
			id := ctx.Param("id")
			ctx.SendText(200, fmt.Sprintf("User is %s", id))
		})
		api.OnGet("/me", func(ctx *zentrox.Context) {
			ctx.SendText(200, "me ok")
		})
	}

	log.Println("listening on :8000")
	_ = app.Run(":8000")
}
