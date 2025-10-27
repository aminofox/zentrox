package main

import (
	"fmt"
	"log"
	"time"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
)

// Example showing minimal Zentrox setup with custom logger integration
// This demonstrates how easy it is to integrate Zentrox into existing projects

func main() {
	app := zentrox.NewApp()

	// Example 1: Use built-in logger
	// app.Plug(middleware.Logger())

	// Example 2: Use your own logger function
	// Perfect for integrating with your existing logging system (zap, logrus, etc.)
	app.Plug(middleware.LoggerWithFunc(customLogger))

	// Example 3: Add only essential middleware
	app.Plug(
		middleware.Recovery(), // Panic recovery
		middleware.ErrorHandler(middleware.DefaultErrorHandler()), // Error handling
	)

	// Basic routes
	app.GET("/", func(c *zentrox.Context) {
		c.JSON(200, map[string]string{
			"message": "Zentrox minimal example",
			"status":  "running",
		})
	})

	app.GET("/hello/:name", func(c *zentrox.Context) {
		name := c.Param("name")
		c.JSON(200, map[string]string{
			"message": fmt.Sprintf("Hello, %s!", name),
		})
	})

	// Protected route with JWT
	secret := []byte("your-secret-key-here")
	protected := app.Scope("/api", middleware.JWT(middleware.JWTConfig{
		Secret:     secret,
		ContextKey: "user",
	}))

	protected.GET("/me", func(c *zentrox.Context) {
		// Get user from JWT token
		user, _ := c.Get("user")
		c.JSON(200, user)
	})

	log.Println("Starting Zentrox minimal server on :8000")
	app.Run(":8000")
}

// customLogger is an example of integrating your own logging system
// You can replace this with calls to zap, logrus, or any other logger
func customLogger(method, path string, status int, duration time.Duration, err error) {
	// This is where you'd call your existing logger
	// For example with zap:
	// logger.Info("request",
	//     zap.String("method", method),
	//     zap.String("path", path),
	//     zap.Int("status", status),
	//     zap.Duration("duration", duration),
	//     zap.Error(err),
	// )

	// Simple example using standard log
	if err != nil {
		log.Printf("[%s] %s - %d (%s) - ERROR: %v", method, path, status, duration, err)
	} else {
		log.Printf("[%s] %s - %d (%s)", method, path, status, duration)
	}
}
