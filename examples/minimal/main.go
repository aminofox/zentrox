package main

import (
	"errors"
	"log"
	"time"

	"github.com/aminofox/zentrox/v2"
	"github.com/aminofox/zentrox/v2/middleware"
)

func main() {
	app := zentrox.NewApp()
	secret := []byte("your-secret-key-here")

	// Swap LoggerWithFunc with your own logger (zap, logrus, etc.)
	app.Plug(
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
		c.JSON(200, map[string]string{"status": "ok"})
	})

	app.GET("/hello/:name", func(c *zentrox.Context) {
		c.JSON(200, map[string]string{"message": "Hello, " + c.Param("name") + "!"})
	})

	// Issue a signed JWT — use the token in: Authorization: Bearer <token>
	app.GET("/token", func(c *zentrox.Context) {
		claims := map[string]any{
			"sub":  "user123",
			"role": "admin",
			"iss":  "myapp",
			"exp":  time.Now().Add(time.Hour).Unix(),
		}
		token, _ := middleware.SignHS256(claims, secret)
		c.JSON(200, map[string]string{"token": token})
	})

	// Protected scope: validates exp, iss, and role
	api := app.Scope("/api", middleware.JWT(middleware.JWTConfig{
		Secret:     secret,
		ContextKey: "user",
		ValidateFunc: func(claims map[string]any) error {
			if exp, ok := claims["exp"].(float64); ok && time.Now().Unix() > int64(exp) {
				return errors.New("token expired")
			}
			if iss, _ := claims["iss"].(string); iss != "myapp" {
				return errors.New("invalid issuer")
			}
			if role, _ := claims["role"].(string); role != "admin" {
				return errors.New("admin role required")
			}
			return nil
		},
	}))

	api.GET("/me", func(c *zentrox.Context) {
		user, _ := c.Get("user")
		c.JSON(200, user)
	})

	log.Println("listening on :8000")
	app.Run(":8000")
}
