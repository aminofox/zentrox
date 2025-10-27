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

// ==================== Domain Models ====================

type User struct {
	ID          int      `json:"id"`
	Email       string   `json:"email"`
	Password    string   `json:"-"`
	Name        string   `json:"name"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

type Product struct {
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	Price      float64 `json:"price"`
	Stock      int     `json:"stock"`
	CategoryID int     `json:"category_id"`
}

type Order struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	ProductID int       `json:"product_id"`
	Quantity  int       `json:"quantity"`
	Total     float64   `json:"total"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// ==================== Services ====================

type Database struct {
	users    map[string]*User
	products map[int]*Product
	orders   map[int]*Order
	mutex    sync.RWMutex
}

func NewDatabase() *Database {
	return &Database{
		users:    make(map[string]*User),
		products: make(map[int]*Product),
		orders:   make(map[int]*Order),
	}
}

type Config struct {
	JWTSecret      []byte
	MaxOrderAmount float64
}

type Logger struct {
	prefix string
}

func (l *Logger) Info(msg string, args ...any) {
	log.Printf("[%s] INFO: %s %v", l.prefix, msg, args)
}

func (l *Logger) Error(msg string, err error) {
	log.Printf("[%s] ERROR: %s - %v", l.prefix, msg, err)
}

type Services struct {
	DB     *Database
	Config *Config
	Logger *Logger
}

// ==================== Middleware ====================

func RoleMiddleware(services *Services, allowedRoles ...string) zentrox.Handler {
	return func(c *zentrox.Context) {
		claims, exists := c.Get("user")
		if !exists {
			c.Fail(401, "Unauthorized")
			return
		}

		claimsMap := claims.(map[string]any)
		userRole, _ := claimsMap["role"].(string)

		for _, role := range allowedRoles {
			if userRole == role {
				services.Logger.Info("Role check passed", "role", userRole)
				c.Next()
				return
			}
		}

		services.Logger.Error("Access denied", fmt.Errorf("role %s not in %v", userRole, allowedRoles))
		c.Fail(403, fmt.Sprintf("Forbidden: requires role %v", allowedRoles))
	}
}

func PermissionMiddleware(services *Services, requiredPerms ...string) zentrox.Handler {
	return func(c *zentrox.Context) {
		claims, _ := c.Get("user")
		claimsMap := claims.(map[string]any)
		userPerms, _ := claimsMap["permissions"].([]any)

		for _, required := range requiredPerms {
			hasPermission := false
			for _, perm := range userPerms {
				if perm.(string) == required {
					hasPermission = true
					break
				}
			}
			if !hasPermission {
				c.Fail(403, fmt.Sprintf("Missing permission: %s", required))
				return
			}
		}
		c.Next()
	}
}

// ==================== Handlers ====================

func handleRegister(c *zentrox.Context, services *Services) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}

	if err := c.BindJSONInto(&req); err != nil {
		c.Fail(400, "Invalid request")
		return
	}

	services.DB.mutex.Lock()
	defer services.DB.mutex.Unlock()

	if _, exists := services.DB.users[req.Email]; exists {
		c.Fail(409, "User already exists")
		return
	}

	user := &User{
		ID:          len(services.DB.users) + 1,
		Email:       req.Email,
		Password:    req.Password,
		Name:        req.Name,
		Role:        "user",
		Permissions: []string{"read:own", "write:own"},
	}
	services.DB.users[req.Email] = user

	services.Logger.Info("User registered", "email", req.Email)
	c.JSON(201, map[string]any{"message": "User registered", "user": user})
}

func handleLogin(c *zentrox.Context, services *Services) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := c.BindJSONInto(&req); err != nil {
		c.Fail(400, "Invalid request")
		return
	}

	services.DB.mutex.RLock()
	user, exists := services.DB.users[req.Email]
	services.DB.mutex.RUnlock()

	if !exists || user.Password != req.Password {
		c.Fail(401, "Invalid credentials")
		return
	}

	perms := make([]any, len(user.Permissions))
	for i, p := range user.Permissions {
		perms[i] = p
	}

	token, _ := middleware.SignHS256(map[string]any{
		"sub":         user.ID,
		"email":       user.Email,
		"name":        user.Name,
		"role":        user.Role,
		"permissions": perms,
		"exp":         time.Now().Add(24 * time.Hour).Unix(),
	}, services.Config.JWTSecret)

	services.Logger.Info("User logged in", "email", req.Email)
	c.JSON(200, map[string]any{"token": token, "user": user})
}

func handleListProducts(c *zentrox.Context, services *Services) {
	services.DB.mutex.RLock()
	products := make([]*Product, 0, len(services.DB.products))
	for _, p := range services.DB.products {
		products = append(products, p)
	}
	services.DB.mutex.RUnlock()

	services.Logger.Info("Products listed", "count", len(products))
	c.JSON(200, map[string]any{"products": products, "total": len(products)})
}

func handleGetProduct(c *zentrox.Context, services *Services) {
	id := c.Param("id")
	services.Logger.Info("Get product", "id", id)
	c.JSON(200, map[string]string{"message": "Product details", "id": id})
}

func handleCreateProduct(c *zentrox.Context, services *Services) {
	var req struct {
		Name       string  `json:"name"`
		Price      float64 `json:"price"`
		Stock      int     `json:"stock"`
		CategoryID int     `json:"category_id"`
	}

	if err := c.BindJSONInto(&req); err != nil {
		c.Fail(400, "Invalid request")
		return
	}

	services.DB.mutex.Lock()
	product := &Product{
		ID:         len(services.DB.products) + 1,
		Name:       req.Name,
		Price:      req.Price,
		Stock:      req.Stock,
		CategoryID: req.CategoryID,
	}
	services.DB.products[product.ID] = product
	services.DB.mutex.Unlock()

	services.Logger.Info("Product created", "id", product.ID)
	c.JSON(201, map[string]any{"message": "Product created", "product": product})
}

func handleUpdateProduct(c *zentrox.Context, services *Services) {
	id := c.Param("id")
	services.Logger.Info("Update product", "id", id)
	c.JSON(200, map[string]string{"message": "Product updated", "id": id})
}

func handleDeleteProduct(c *zentrox.Context, services *Services) {
	id := c.Param("id")
	services.Logger.Info("Delete product", "id", id)
	c.JSON(200, map[string]string{"message": "Product deleted", "id": id})
}

func handleListOrders(c *zentrox.Context, services *Services) {
	claims, _ := c.Get("user")
	claimsMap := claims.(map[string]any)
	userID := int(claimsMap["sub"].(float64))

	services.DB.mutex.RLock()
	orders := make([]*Order, 0)
	for _, o := range services.DB.orders {
		if o.UserID == userID {
			orders = append(orders, o)
		}
	}
	services.DB.mutex.RUnlock()

	services.Logger.Info("Orders listed", "user", userID)
	c.JSON(200, map[string]any{"orders": orders, "total": len(orders)})
}

func handleCreateOrder(c *zentrox.Context, services *Services) {
	claims, _ := c.Get("user")
	claimsMap := claims.(map[string]any)
	userID := int(claimsMap["sub"].(float64))

	var req struct {
		ProductID int `json:"product_id"`
		Quantity  int `json:"quantity"`
	}

	if err := c.BindJSONInto(&req); err != nil {
		c.Fail(400, "Invalid request")
		return
	}

	services.DB.mutex.Lock()
	order := &Order{
		ID:        len(services.DB.orders) + 1,
		UserID:    userID,
		ProductID: req.ProductID,
		Quantity:  req.Quantity,
		Total:     999.99,
		Status:    "pending",
		CreatedAt: time.Now(),
	}
	services.DB.orders[order.ID] = order
	services.DB.mutex.Unlock()

	services.Logger.Info("Order created", "id", order.ID)
	c.JSON(201, map[string]any{"message": "Order created", "order": order})
}

func handleGetOrder(c *zentrox.Context, services *Services) {
	id := c.Param("id")
	c.JSON(200, map[string]string{"message": "Order details", "id": id})
}

func handleAdminDashboard(c *zentrox.Context, services *Services) {
	services.DB.mutex.RLock()
	stats := map[string]any{
		"total_users":    len(services.DB.users),
		"total_products": len(services.DB.products),
		"total_orders":   len(services.DB.orders),
	}
	services.DB.mutex.RUnlock()
	c.JSON(200, stats)
}

func handleAdminAllOrders(c *zentrox.Context, services *Services) {
	services.DB.mutex.RLock()
	orders := make([]*Order, 0, len(services.DB.orders))
	for _, o := range services.DB.orders {
		orders = append(orders, o)
	}
	services.DB.mutex.RUnlock()
	c.JSON(200, map[string]any{"orders": orders, "total": len(orders)})
}

func handleAdminUpdateOrder(c *zentrox.Context, services *Services) {
	id := c.Param("id")
	c.JSON(200, map[string]string{"message": "Order updated by admin", "id": id})
}

func handleManagerStats(c *zentrox.Context, services *Services) {
	c.JSON(200, map[string]any{"revenue": 50000, "pending_orders": 25})
}

func handleInventoryReport(c *zentrox.Context, services *Services) {
	year, month := c.Param("year"), c.Param("month")
	c.JSON(200, map[string]any{"year": year, "month": month, "low_stock_items": 5})
}

// ==================== Main ====================

func main() {
	services := &Services{
		DB:     NewDatabase(),
		Config: &Config{JWTSecret: []byte("super-secret-key"), MaxOrderAmount: 10000},
		Logger: &Logger{prefix: "API"},
	}

	app := zentrox.NewApp()
	app.Plug(
		middleware.Logger(),
		middleware.Recovery(),
		middleware.CORS(middleware.DefaultCORS()),
	)

	app.GET("/", func(c *zentrox.Context) {
		c.JSON(200, map[string]string{"message": "Enterprise API"})
	})

	authGroup := app.Scope("/auth")
	{
		authGroup.POST("/register", func(c *zentrox.Context) {
			handleRegister(c, services)
		})
		authGroup.POST("/login", func(c *zentrox.Context) {
			handleLogin(c, services)
		})
	}

	apiGroup := app.Scope("/api", middleware.JWT(middleware.JWTConfig{
		Secret:     services.Config.JWTSecret,
		ContextKey: "user",
		ValidateFunc: func(claims map[string]any) error {
			if exp, ok := claims["exp"].(float64); ok {
				if time.Now().Unix() > int64(exp) {
					return errors.New("token expired")
				}
			}
			return nil
		},
	}))
	{
		productsGroup := apiGroup.Scope("/products")
		{
			productsGroup.GET("", func(c *zentrox.Context) {
				handleListProducts(c, services)
			})
			productsGroup.GET("/:id", func(c *zentrox.Context) {
				handleGetProduct(c, services)
			})

			productsWriteGroup := productsGroup.Scope("")
			productsWriteGroup.Use(RoleMiddleware(services, "admin", "manager"))
			{
				productsWriteGroup.POST("", func(c *zentrox.Context) {
					handleCreateProduct(c, services)
				})
				productsWriteGroup.PUT("/:id", func(c *zentrox.Context) {
					handleUpdateProduct(c, services)
				})
			}

			productsDeleteGroup := productsGroup.Scope("")
			productsDeleteGroup.Use(RoleMiddleware(services, "admin"))
			{
				productsDeleteGroup.DELETE("/:id", func(c *zentrox.Context) {
					handleDeleteProduct(c, services)
				})
			}
		}

		ordersGroup := apiGroup.Scope("/orders")
		{
			ordersGroup.GET("", func(c *zentrox.Context) {
				handleListOrders(c, services)
			})
			ordersGroup.POST("", func(c *zentrox.Context) {
				handleCreateOrder(c, services)
			})
			ordersGroup.GET("/:id", func(c *zentrox.Context) {
				handleGetOrder(c, services)
			})
		}

		adminGroup := apiGroup.Scope("/admin", RoleMiddleware(services, "admin"))
		{
			adminGroup.GET("/dashboard", func(c *zentrox.Context) {
				handleAdminDashboard(c, services)
			})

			adminOrdersGroup := adminGroup.Scope("/orders")
			{
				adminOrdersGroup.GET("", func(c *zentrox.Context) {
					handleAdminAllOrders(c, services)
				})
				adminOrdersGroup.PUT("/:id", func(c *zentrox.Context) {
					handleAdminUpdateOrder(c, services)
				})
			}

			adminProductsGroup := adminGroup.Scope("/products")
			{
				adminProductsGroup.GET("/low-stock", func(c *zentrox.Context) {
					handleListProducts(c, services)
				})
			}
		}

		managerGroup := apiGroup.Scope("/manager", RoleMiddleware(services, "manager", "admin"))
		{
			managerGroup.GET("/stats", func(c *zentrox.Context) {
				handleManagerStats(c, services)
			})

			reportsGroup := managerGroup.Scope("/reports")
			{
				reportsGroup.GET("/inventory/:year/:month", func(c *zentrox.Context) {
					handleInventoryReport(c, services)
				})
			}
		}

		salesGroup := apiGroup.Scope("/sales", PermissionMiddleware(services, "read:sales"))
		{
			salesGroup.GET("/revenue", func(c *zentrox.Context) {
				c.JSON(200, map[string]float64{"revenue": 100000})
			})
		}
	}

	log.Println("================================================================")
	log.Println("🚀 Enterprise API")
	log.Println("================================================================")
	log.Println("✓ Handlers receive dependencies as parameters")
	log.Println("✓ Nested router groups with {}")
	log.Println("✓ Inline role checks with middleware")
	log.Println("================================================================")
	log.Println("Server: http://localhost:8000")
	log.Println("================================================================")

	app.Run(":8000")
}
