package main

import (
	"html"
	"log"
	"os"
	"time"

	"github.com/aminofox/zentrox/v2"
	"github.com/aminofox/zentrox/v2/middleware"
)

func main() {
	app := zentrox.NewApp()
	secret := []byte(os.Getenv("JWT_SECRET"))
	if len(secret) == 0 {
		log.Fatal("JWT_SECRET is required")
	}

	// Swap LoggerWithFunc with your own logger (zap, logrus, etc.)
	app.Use(
		middleware.LoggerWithFunc(func(method, path string, status int, duration time.Duration, err error) {
			if err != nil {
				log.Printf("[%s] %s %d (%s) err=%v", method, path, status, duration, err)
			} else {
				log.Printf("[%s] %s %d (%s)", method, path, status, duration)
			}
		}),
		middleware.Recovery(),
		middleware.ErrorHandler(middleware.DefaultErrorHandler()),
	)

	app.GET("/", func(c *zentrox.Context) {
		_ = c.JSON(200, map[string]string{"status": "ok"})
	})

	app.GET("/hello/:name", func(c *zentrox.Context) {
		name := html.EscapeString(c.Param("name"))
		_ = c.JSON(200, map[string]string{"message": "Hello, " + name + "!"})
	})

	// Issue a signed JWT — use the token in: Authorization: Bearer <token>
	app.GET("/token", func(c *zentrox.Context) {
		claims := &middleware.JWTClaims{
			RegisteredClaims: middleware.RegisteredClaims{
				Subject:   "user123",
				ExpiresAt: time.Now().Add(1 * time.Hour).Unix(),
			},
		}
		token, _ := middleware.SignHS256(claims, secret)
		_ = c.JSON(200, map[string]string{"token": token})
	})

	// Protected group: validates the token signature and registered time claims.
	api := app.Group("/api", middleware.JWT(middleware.JWTConfig{
		Secret:     secret,
		ContextKey: "user",
	}))

	api.GET("/me", func(c *zentrox.Context) {
		user, _ := c.Get("user")
		_ = c.JSON(200, user)
	})

	log.Println("listening on :8000")
	_ = app.Run(":8000")
}
