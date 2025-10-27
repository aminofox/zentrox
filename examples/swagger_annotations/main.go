package main

import (
	"log"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"

	_ "github.com/aminofox/zentrox/examples/swagger_annotations/docs"
)

// @title           Zentrox API Example
// @version         1.0
// @description     This is a sample server using Zentrox with Swagger annotations
// @termsOfService  http://swagger.io/terms/

// @contact.name   API Support
// @contact.url    http://www.swagger.io/support
// @contact.email  support@swagger.io

// @license.name  Apache 2.0
// @license.url   http://www.apache.org/licenses/LICENSE-2.0.html

// @host      localhost:8000
// @BasePath  /api/v1

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Type "Bearer" followed by a space and JWT token.

type User struct {
	ID    int    `json:"id" example:"1"`
	Name  string `json:"name" example:"John Doe"`
	Email string `json:"email" example:"john@example.com"`
	Age   int    `json:"age" example:"25"`
}

type CreateUserRequest struct {
	Name  string `json:"name" binding:"required" example:"John Doe"`
	Email string `json:"email" binding:"required,email" example:"john@example.com"`
	Age   int    `json:"age" binding:"required,gte=0,lte=130" example:"25"`
}

type UpdateUserRequest struct {
	Name  string `json:"name" example:"John Doe"`
	Email string `json:"email" example:"john@example.com"`
	Age   int    `json:"age" example:"25"`
}

type ErrorResponse struct {
	Code    int    `json:"code" example:"400"`
	Message string `json:"message" example:"Bad request"`
}

type ListUsersResponse struct {
	Users []User `json:"users"`
	Total int    `json:"total" example:"10"`
	Page  int    `json:"page" example:"1"`
	Limit int    `json:"limit" example:"10"`
}

func main() {
	app := zentrox.NewApp()

	app.Plug(middleware.Recovery())
	app.Plug(middleware.Logger())
	app.Plug(middleware.CORS(middleware.DefaultCORS()))

	// Serve Swagger UI at /swagger/*
	// After running 'swag init', open: http://localhost:8000/swagger/index.html
	app.ServeSwagger("/swagger")

	// API v1 routes
	v1 := app.Scope("/api/v1")
	{
		users := v1.Scope("/users")
		{
			users.GET("", ListUsers)
			users.GET("/:id", GetUser)
			users.POST("", CreateUser)
			users.PUT("/:id", UpdateUser)
			users.DELETE("/:id", DeleteUser)
		}

		// Protected routes
		protected := v1.Scope("/protected")
		protected.Use(AuthMiddleware())
		{
			protected.GET("/profile", GetProfile)
		}
	}

	// Health check
	app.GET("/health", HealthCheck)

	log.Println("════════════════════════════════════════════════")
	log.Println("  Server starting on :8000")
	log.Println("════════════════════════════════════════════════")
	log.Println("  📚 Swagger UI: http://localhost:8000/swagger/index.html")
	log.Println("")
	log.Println("  Steps to generate Swagger docs:")
	log.Println("  1. Install: go install github.com/swaggo/swag/cmd/swag@latest")
	log.Println("  2. Generate: cd examples/swagger_annotations && swag init")
	log.Println("  3. Run: go run main.go")
	log.Println("════════════════════════════════════════════════")

	if err := app.Run(":8000"); err != nil {
		log.Fatal(err)
	}
} // AuthMiddleware checks for Bearer token
func AuthMiddleware() zentrox.Handler {
	return func(c *zentrox.Context) {
		auth := c.GetHeader("Authorization")
		if auth == "" {
			c.JSON(401, ErrorResponse{
				Code:    401,
				Message: "Authorization header required",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

// HealthCheck godoc
// @Summary      Health check
// @Description  Check if the API is running
// @Tags         health
// @Produce      json
// @Success      200  {object}  map[string]string
// @Router       /health [get]
func HealthCheck(c *zentrox.Context) {
	c.JSON(200, map[string]string{
		"status": "ok",
	})
}

// ListUsers godoc
// @Summary      List users
// @Description  Get list of users with pagination
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        page   query     int  false  "Page number"  default(1)
// @Param        limit  query     int  false  "Items per page"  default(10)
// @Param        search query     string  false  "Search by name or email"
// @Success      200  {object}  ListUsersResponse
// @Failure      400  {object}  ErrorResponse
// @Router       /api/v1/users [get]
func ListUsers(c *zentrox.Context) {
	// Mock data
	users := []User{
		{ID: 1, Name: "John Doe", Email: "john@example.com", Age: 25},
		{ID: 2, Name: "Jane Smith", Email: "jane@example.com", Age: 30},
	}

	c.JSON(200, ListUsersResponse{
		Users: users,
		Total: len(users),
		Page:  1,
		Limit: 10,
	})
}

// GetUser godoc
// @Summary      Get user by ID
// @Description  Get a single user by their ID
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "User ID"
// @Success      200  {object}  User
// @Failure      404  {object}  ErrorResponse
// @Router       /api/v1/users/{id} [get]
func GetUser(c *zentrox.Context) {
	_ = c.Param("id") // Mock: just acknowledge the ID

	// Mock data
	user := User{
		ID:    1,
		Name:  "John Doe",
		Email: "john@example.com",
		Age:   25,
	}

	c.JSON(200, user)
}

// CreateUser godoc
// @Summary      Create new user
// @Description  Create a new user with the provided data
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        user  body      CreateUserRequest  true  "User data"
// @Success      201   {object}  User
// @Failure      400   {object}  ErrorResponse
// @Router       /api/v1/users [post]
func CreateUser(c *zentrox.Context) {
	var req CreateUserRequest
	if err := c.BindJSONInto(&req); err != nil {
		c.JSON(400, ErrorResponse{
			Code:    400,
			Message: err.Error(),
		})
		return
	}

	// Mock created user
	user := User{
		ID:    1,
		Name:  req.Name,
		Email: req.Email,
		Age:   req.Age,
	}

	c.JSON(201, user)
}

// UpdateUser godoc
// @Summary      Update user
// @Description  Update an existing user by ID
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        id    path      int                true  "User ID"
// @Param        user  body      UpdateUserRequest  true  "User data"
// @Success      200   {object}  User
// @Failure      400   {object}  ErrorResponse
// @Failure      404   {object}  ErrorResponse
// @Router       /api/v1/users/{id} [put]
func UpdateUser(c *zentrox.Context) {
	_ = c.Param("id") // Mock: just acknowledge the ID

	var req UpdateUserRequest
	if err := c.BindJSONInto(&req); err != nil {
		c.JSON(400, ErrorResponse{
			Code:    400,
			Message: err.Error(),
		})
		return
	}

	// Mock updated user
	user := User{
		ID:    1,
		Name:  req.Name,
		Email: req.Email,
		Age:   req.Age,
	}

	c.JSON(200, user)
}

// DeleteUser godoc
// @Summary      Delete user
// @Description  Delete a user by ID
// @Tags         users
// @Accept       json
// @Produce      json
// @Param        id   path      int  true  "User ID"
// @Success      204  {object}  nil
// @Failure      404  {object}  ErrorResponse
// @Router       /api/v1/users/{id} [delete]
func DeleteUser(c *zentrox.Context) {
	id := c.Param("id")
	_ = id // Mock: just acknowledge the ID

	c.JSON(204, nil)
}

// GetProfile godoc
// @Summary      Get user profile
// @Description  Get the profile of the authenticated user
// @Tags         users
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  User
// @Failure      401  {object}  ErrorResponse
// @Router       /api/v1/protected/profile [get]
func GetProfile(c *zentrox.Context) {
	// Mock authenticated user
	user := User{
		ID:    1,
		Name:  "John Doe",
		Email: "john@example.com",
		Age:   25,
	}

	c.JSON(200, user)
}
