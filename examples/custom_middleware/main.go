package main

import (
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
		c.Next()
	}
}

func AfterAuthGuard() zentrox.Handler {
	return func(c *zentrox.Context) {
		log.Println("AfterAuthGuard")
		c.Next()
	}
}

func main() {
	app := zentrox.NewApp()

	// global middlewares
	app.Plug(
		middleware.ErrorHandler(middleware.DefaultErrorHandler()),
	)

	app.GET("/public", func(c *zentrox.Context) {
		c.String(200, "public ok")
	})

	app.GET("/secure", AuthGuard(), AfterAuthGuard(), (func(c *zentrox.Context) {
		c.String(200, "secure ok")
	}))

	api := app.Scope("api", AuthGuard())
	{
		api.GET("/users", func(ctx *zentrox.Context) {
			ctx.String(200, "list ok")
		})
		api.GET("/user/:id", AfterAuthGuard(), func(ctx *zentrox.Context) {
			id := ctx.Param("id")
			ctx.String(200, "User is %s", id)
		})
		api.GET("/me", func(ctx *zentrox.Context) {
			ctx.String(200, "me ok")
		})
	}

	log.Println("listening on :8000")
	_ = app.Run(":8000")
}
