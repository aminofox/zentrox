package main

import (
	"log"
	"net/http"
	"time"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
)

func main() {
	app := zentrox.NewApp()

	app.Plug(
		middleware.CORS(middleware.DefaultCORS()),
	)

	secret := []byte("supersecret")

	auth := app.Scope("/auth", middleware.JWT(middleware.JWTConfig{
		Secret:     secret,
		ContextKey: "user",
	}))

	app.GET("/", func(c *zentrox.Context) {
		c.String(200, "public ok")
	})

	app.GET("/token", func(c *zentrox.Context) {
		now := time.Now().Unix()
		tok, _ := middleware.SignHS256(map[string]any{
			"sub":  "123",
			"name": "demo",
			"exp":  now + 3600,
			"iss":  "https://issuer.example.com",
			"aud":  "api://zentrox",
		}, secret)
		c.JSON(200, map[string]any{"token": tok})
	})

	auth.GET("/me", func(c *zentrox.Context) {
		u, _ := c.Get("user")
		c.JSON(http.StatusOK, map[string]any{"user": u})
	})

	log.Println("listening on :8000")
	_ = app.Run(":8000")
}
