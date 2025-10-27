package main

import (
	"log"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
)

type Services struct {
	DB     string
	Cache  string
	Logger string
}

func AuthMiddleware(c *zentrox.Context) {
	token := c.GetHeader("Authorization")
	if token == "" {
		c.JSON(401, map[string]string{"error": "unauthorized"})
		c.Abort()
		return
	}
	c.Next()
}

func RoleMiddleware(services *Services, allowedRoles ...string) zentrox.Handler {
	return func(c *zentrox.Context) {
		userRole := c.GetHeader("X-User-Role")

		allowed := false
		for _, role := range allowedRoles {
			if userRole == role {
				allowed = true
				break
			}
		}

		if !allowed {
			services.Logger = "forbidden access attempt"
			c.JSON(403, map[string]string{"error": "forbidden"})
			c.Abort()
			return
		}
		c.Next()
	}
}

func main() {
	app := zentrox.NewApp()

	// Initialize services
	services := &Services{
		DB:     "postgres://localhost",
		Cache:  "redis://localhost",
		Logger: "logger initialized",
	}

	// Global middleware
	app.Plug(middleware.Recovery())
	app.Plug(middleware.Logger())

	app.GET("/", func(c *zentrox.Context) {
		c.JSON(200, map[string]string{"message": "public home"})
	})

	app.GET("/health", func(c *zentrox.Context) {
		c.JSON(200, map[string]string{"status": "ok"})
	})

	apiGroup := app.Scope("/api")
	apiGroup.Use(AuthMiddleware)
	{
		productsGroup := apiGroup.Scope("/products")
		{
			productsGroup.GET("", func(c *zentrox.Context) {
				c.JSON(200, map[string]interface{}{
					"products": []string{"Product 1", "Product 2"},
					"db":       services.DB,
				})
			})

			productsGroup.GET("/:id", func(c *zentrox.Context) {
				id := c.Param("id")
				c.JSON(200, map[string]interface{}{
					"id":   id,
					"name": "Product " + id,
				})
			})

			productsWriteGroup := productsGroup.Scope("")
			productsWriteGroup.Use(RoleMiddleware(services, "admin", "manager"))
			{
				productsWriteGroup.POST("", func(c *zentrox.Context) {
					c.JSON(201, map[string]string{
						"message": "product created",
						"logger":  services.Logger,
					})
				})

				productsWriteGroup.PUT("/:id", func(c *zentrox.Context) {
					id := c.Param("id")
					c.JSON(200, map[string]string{
						"message": "product " + id + " updated",
					})
				})
			}

			productsDeleteGroup := productsGroup.Scope("")
			productsDeleteGroup.Use(RoleMiddleware(services, "admin"))
			{
				productsDeleteGroup.DELETE("/:id", func(c *zentrox.Context) {
					id := c.Param("id")
					c.JSON(200, map[string]string{
						"message": "product " + id + " deleted",
					})
				})
			}
		}

		adminGroup := apiGroup.Scope("/admin")
		adminGroup.Use(RoleMiddleware(services, "admin"))
		{
			adminGroup.GET("/dashboard", func(c *zentrox.Context) {
				c.JSON(200, map[string]interface{}{
					"stats": map[string]int{
						"users":    100,
						"products": 50,
						"orders":   200,
					},
					"cache": services.Cache,
				})
			})

			adminGroup.GET("/users", func(c *zentrox.Context) {
				c.JSON(200, map[string]interface{}{
					"users": []string{"user1", "user2"},
				})
			})

			settingsGroup := adminGroup.Scope("/settings")
			{
				settingsGroup.GET("", func(c *zentrox.Context) {
					c.JSON(200, map[string]string{
						"setting": "value",
					})
				})

				settingsGroup.PUT("", func(c *zentrox.Context) {
					c.JSON(200, map[string]string{
						"message": "settings updated",
					})
				})
			}
		}
	}

	log.Println("Server starting on :8000")
	log.Println("\nTest commands:")
	log.Println("Public:  curl http://localhost:8000/")
	log.Println("Auth:    curl -H 'Authorization: Bearer token' http://localhost:8000/api/products")
	log.Println("Manager: curl -H 'Authorization: Bearer token' -H 'X-User-Role: manager' -X POST http://localhost:8000/api/products")
	log.Println("Admin:   curl -H 'Authorization: Bearer token' -H 'X-User-Role: admin' http://localhost:8000/api/admin/dashboard")

	if err := app.Run(":8000"); err != nil {
		log.Fatal(err)
	}
}
