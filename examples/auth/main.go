package main

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
)

// User model
type User struct {
	ID       int    `json:"id"`
	Email    string `json:"email"`
	Password string `json:"-"` // Never send password in response
	Name     string `json:"name"`
	Role     string `json:"role"` // admin, user
}

// In-memory database (use real database in production)
var (
	users      = make(map[string]*User) // email -> user
	usersMutex sync.RWMutex
	userIDSeq  = 1
)

// DTOs (Data Transfer Objects)
type RegisterRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Name     string `json:"name" binding:"required"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type LoginResponse struct {
	Token string `json:"token"`
	User  *User  `json:"user"`
}

func main() {
	app := zentrox.NewApp()
	secret := []byte("super-secret-jwt-key-change-in-production")

	// Global middleware
	app.Plug(
		middleware.Logger(),
		middleware.Recovery(),
		middleware.CORS(middleware.DefaultCORS()),
	)

	// Public routes
	app.GET("/", func(c *zentrox.Context) {
		c.JSON(200, map[string]string{
			"message": "Auth API - Register at /auth/register, Login at /auth/login",
		})
	})

	// Auth routes (public - no JWT required)
	auth := app.Scope("/auth")
	auth.POST("/register", handleRegister)
	auth.POST("/login", handleLogin(secret))

	// Protected API routes (JWT required)
	api := app.Scope("/api", middleware.JWT(middleware.JWTConfig{
		Secret:     secret,
		ContextKey: "user",
		ValidateFunc: func(claims map[string]any) error {
			// Check token expiry
			if exp, ok := claims["exp"].(float64); ok {
				if time.Now().Unix() > int64(exp) {
					return errors.New("token expired")
				}
			}
			return nil
		},
	}))

	// User routes - /api/users/*
	users := api.Scope("/users")
	users.GET("", handleListUsers)         // GET /api/users
	users.GET("/:id", handleGetUser)       // GET /api/users/:id
	users.PUT("/:id", handleUpdateUser)    // PUT /api/users/:id
	users.DELETE("/:id", handleDeleteUser) // DELETE /api/users/:id

	// Profile routes (current user) - /api/profile/*
	profile := api.Scope("/profile")
	profile.GET("", handleGetProfile)           // GET /api/profile
	profile.PUT("", handleUpdateProfile)        // PUT /api/profile
	profile.POST("/avatar", handleUploadAvatar) // POST /api/profile/avatar

	// Admin routes (admin only) - /api/admin/*
	admin := api.Scope("/admin", adminMiddleware())
	admin.GET("/stats", handleAdminStats) // GET /api/admin/stats
	admin.GET("/logs", handleAdminLogs)   // GET /api/admin/logs

	// Nested admin groups
	adminUsers := admin.Scope("/users")
	adminUsers.GET("", handleAdminListUsers)       // GET /api/admin/users
	adminUsers.POST("/:id/ban", handleBanUser)     // POST /api/admin/users/:id/ban
	adminUsers.POST("/:id/unban", handleUnbanUser) // POST /api/admin/users/:id/unban

	adminSettings := admin.Scope("/settings")
	adminSettings.GET("", handleGetSettings)    // GET /api/admin/settings
	adminSettings.PUT("", handleUpdateSettings) // PUT /api/admin/settings

	// Deep nested example - /api/admin/reports/monthly/2024/10
	adminReports := admin.Scope("/reports")
	monthlyReports := adminReports.Scope("/monthly")
	monthlyReports.GET("/:year/:month", handleMonthlyReport)

	log.Println("=================================================")
	log.Println("🚀 Auth API Server started on http://localhost:8000")
	log.Println("=================================================")
	log.Println("Public endpoints:")
	log.Println("  POST /auth/register - Register new user")
	log.Println("  POST /auth/login    - Login and get JWT token")
	log.Println("")
	log.Println("Protected endpoints (requires JWT token):")
	log.Println("  GET    /api/users           - List all users")
	log.Println("  GET    /api/users/:id       - Get user by ID")
	log.Println("  GET    /api/profile         - Get current user profile")
	log.Println("  PUT    /api/profile         - Update profile")
	log.Println("")
	log.Println("Admin endpoints (requires admin role):")
	log.Println("  GET    /api/admin/stats     - Admin statistics")
	log.Println("  GET    /api/admin/users     - Admin user management")
	log.Println("  POST   /api/admin/users/:id/ban - Ban user")
	log.Println("  GET    /api/admin/reports/monthly/:year/:month")
	log.Println("=================================================")
	log.Println("")
	log.Println("Try it:")
	log.Println(`  curl -X POST http://localhost:8000/auth/register -H "Content-Type: application/json" -d '{"email":"user@example.com","password":"password123","name":"John Doe"}'`)
	log.Println(`  curl -X POST http://localhost:8000/auth/login -H "Content-Type: application/json" -d '{"email":"user@example.com","password":"password123"}'`)
	log.Println("")

	app.Run(":8000")
}

// Handlers

func handleRegister(c *zentrox.Context) {
	var req RegisterRequest
	if err := c.BindJSONInto(&req); err != nil {
		c.Fail(400, "Invalid request: "+err.Error())
		return
	}

	usersMutex.Lock()
	defer usersMutex.Unlock()

	// Check if user exists
	if _, exists := users[req.Email]; exists {
		c.Fail(409, "User already exists")
		return
	}

	// Create new user (in production, hash the password!)
	user := &User{
		ID:       userIDSeq,
		Email:    req.Email,
		Password: req.Password, // In production: bcrypt.GenerateFromPassword()
		Name:     req.Name,
		Role:     "user", // default role
	}
	userIDSeq++
	users[req.Email] = user

	c.JSON(201, map[string]any{
		"message": "User registered successfully",
		"user":    user,
	})
}

func handleLogin(secret []byte) zentrox.Handler {
	return func(c *zentrox.Context) {
		var req LoginRequest
		if err := c.BindJSONInto(&req); err != nil {
			c.Fail(400, "Invalid request: "+err.Error())
			return
		}

		usersMutex.RLock()
		user, exists := users[req.Email]
		usersMutex.RUnlock()

		if !exists {
			c.Fail(401, "Invalid email or password")
			return
		}

		// Check password (in production: bcrypt.CompareHashAndPassword())
		if user.Password != req.Password {
			c.Fail(401, "Invalid email or password")
			return
		}

		// Generate JWT token
		claims := map[string]any{
			"sub":   user.ID,
			"email": user.Email,
			"name":  user.Name,
			"role":  user.Role,
			"exp":   time.Now().Add(24 * time.Hour).Unix(),
			"iss":   "zentrox-auth-api",
		}

		token, _ := middleware.SignHS256(claims, secret)

		c.JSON(200, LoginResponse{
			Token: token,
			User:  user,
		})
	}
}

func handleListUsers(c *zentrox.Context) {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	userList := make([]*User, 0, len(users))
	for _, u := range users {
		userList = append(userList, u)
	}

	c.JSON(200, map[string]any{
		"users": userList,
		"total": len(userList),
	})
}

func handleGetUser(c *zentrox.Context) {
	id := c.Param("id")

	usersMutex.RLock()
	defer usersMutex.RUnlock()

	for _, u := range users {
		if fmt.Sprintf("%d", u.ID) == id {
			c.JSON(200, u)
			return
		}
	}

	c.Fail(404, "User not found")
}

func handleUpdateUser(c *zentrox.Context) {
	c.JSON(200, map[string]string{
		"message": "User updated (not implemented in demo)",
	})
}

func handleDeleteUser(c *zentrox.Context) {
	c.JSON(200, map[string]string{
		"message": "User deleted (not implemented in demo)",
	})
}

func handleGetProfile(c *zentrox.Context) {
	claims, _ := c.Get("user")
	c.JSON(200, map[string]any{
		"profile": claims,
		"message": "This is your profile from JWT claims",
	})
}

func handleUpdateProfile(c *zentrox.Context) {
	claims, _ := c.Get("user")
	c.JSON(200, map[string]any{
		"message": "Profile updated (not implemented in demo)",
		"user":    claims,
	})
}

func handleUploadAvatar(c *zentrox.Context) {
	c.JSON(200, map[string]string{
		"message": "Avatar uploaded (not implemented in demo)",
	})
}

func handleAdminStats(c *zentrox.Context) {
	usersMutex.RLock()
	totalUsers := len(users)
	usersMutex.RUnlock()

	c.JSON(200, map[string]any{
		"total_users":     totalUsers,
		"active_sessions": 42, // mock data
		"requests_today":  1337,
	})
}

func handleAdminLogs(c *zentrox.Context) {
	c.JSON(200, map[string]any{
		"logs": []string{
			"2024-10-27 10:00:00 - User login: user@example.com",
			"2024-10-27 10:05:00 - User registration: admin@example.com",
		},
	})
}

func handleAdminListUsers(c *zentrox.Context) {
	usersMutex.RLock()
	defer usersMutex.RUnlock()

	userList := make([]*User, 0, len(users))
	for _, u := range users {
		userList = append(userList, u)
	}

	c.JSON(200, map[string]any{
		"message": "Admin user list with full details",
		"users":   userList,
		"total":   len(userList),
	})
}

func handleBanUser(c *zentrox.Context) {
	id := c.Param("id")
	c.JSON(200, map[string]string{
		"message": fmt.Sprintf("User %s banned (not implemented in demo)", id),
	})
}

func handleUnbanUser(c *zentrox.Context) {
	id := c.Param("id")
	c.JSON(200, map[string]string{
		"message": fmt.Sprintf("User %s unbanned (not implemented in demo)", id),
	})
}

func handleGetSettings(c *zentrox.Context) {
	c.JSON(200, map[string]any{
		"settings": map[string]any{
			"maintenance_mode": false,
			"max_upload_size":  10485760, // 10MB
			"enable_signup":    true,
		},
	})
}

func handleUpdateSettings(c *zentrox.Context) {
	c.JSON(200, map[string]string{
		"message": "Settings updated (not implemented in demo)",
	})
}

func handleMonthlyReport(c *zentrox.Context) {
	year := c.Param("year")
	month := c.Param("month")

	c.JSON(200, map[string]any{
		"report": map[string]any{
			"year":         year,
			"month":        month,
			"new_users":    125,
			"revenue":      50000,
			"active_users": 1500,
		},
	})
}

// Admin middleware - check if user has admin role
func adminMiddleware() zentrox.Handler {
	return func(c *zentrox.Context) {
		claims, exists := c.Get("user")
		if !exists {
			c.Fail(401, "Unauthorized")
			return
		}

		claimsMap, ok := claims.(map[string]any)
		if !ok {
			c.Fail(401, "Invalid token")
			return
		}

		role, _ := claimsMap["role"].(string)
		if role != "admin" {
			c.Fail(403, "Forbidden: Admin access required")
			return
		}

		c.Next()
	}
}
