package main

import (
	"errors"
	"log"
	"time"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
)

func main() {
	app := zentrox.NewApp()
	secret := []byte("my-secret-key")

	app.Plug(middleware.Logger())

	app.GET("/token", func(c *zentrox.Context) {
		claims := map[string]any{
			"sub":   "user123",
			"name":  "John Doe",
			"role":  "admin",
			"exp":   time.Now().Add(time.Hour).Unix(),
			"iss":   "myapp",
			"email": "john@example.com",
		}
		token, _ := middleware.SignHS256(claims, secret)
		c.JSON(200, map[string]string{"token": token})
	})

	api := app.Scope("/api", middleware.JWT(middleware.JWTConfig{
		Secret:     secret,
		ContextKey: "user",
		ValidateFunc: func(claims map[string]any) error {
			if exp, ok := claims["exp"].(float64); ok {
				if time.Now().Unix() > int64(exp) {
					return errors.New("token expired")
				}
			}

			if iss, ok := claims["iss"].(string); !ok || iss != "myapp" {
				return errors.New("invalid issuer")
			}

			if role, ok := claims["role"].(string); !ok || role != "admin" {
				return errors.New("admin role required")
			}

			return nil
		},
	}))

	api.GET("/profile", func(c *zentrox.Context) {
		user, _ := c.Get("user")
		c.JSON(200, user)
	})

	api.GET("/admin", func(c *zentrox.Context) {
		c.JSON(200, map[string]string{
			"message": "Admin access granted",
		})
	})

	log.Println("JWT custom validation example on :8000")
	app.Run(":8000")
}
