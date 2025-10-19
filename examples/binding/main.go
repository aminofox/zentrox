package main

import (
	"log"
	"net/http"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
)

type CreateUserDTO struct {
	Name  string `json:"name" form:"name"  validate:"required,min=2,max=64"`
	Email string `json:"email" form:"email" validate:"required"`
	Age   int    `json:"age"  form:"age"   validate:"min=1,max=150"`
}

type SearchDTO struct {
	Q     string   `json:"q" query:"q" validate:"required,min=1"`
	Limit int      `json:"limit" query:"limit" validate:"min=1,max=100"`
	Tags  []string `json:"tags" query:"tag"`
}

func main() {
	app := zentrox.NewApp()
	app.Plug(
		middleware.CORS(middleware.CORSConfig{
			AllowOrigins:     []string{"http://localhost:5173", "*"},
			AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowHeaders:     []string{"Content-Type", "Authorization"},
			AllowCredentials: false,
			MaxAge:           3600,
		}),
		middleware.Recovery(),
		middleware.Logger(),
	)

	app.OnPost("/users", func(c *zentrox.Context) {
		var in CreateUserDTO
		if err := c.BindInto(&in); err != nil {
			c.SendJSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		c.SendJSON(http.StatusOK, map[string]any{"created": in})
	})

	app.OnGet("/search", func(c *zentrox.Context) {
		var q SearchDTO
		if err := c.BindQueryInto(&q); err != nil {
			c.SendJSON(http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		c.SendJSON(http.StatusOK, map[string]any{"query": q})
	})

	log.Println("listening on :8000")
	_ = app.Run(":8000")
}
