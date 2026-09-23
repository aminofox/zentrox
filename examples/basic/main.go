package main

import (
	"context"
	"fmt"
	"html"
	"log"
	"net/http"
	"time"

	"github.com/aminofox/zentrox/v2"
	"github.com/aminofox/zentrox/v2/middleware"
)

func handleLogic(ctx context.Context, param, requestID string) string {
	return fmt.Sprintf(`[handleLogic] with param %s and requestID %s`, param, requestID)
}

func main() {
	app := zentrox.NewApp()

	app.Use(
		middleware.CORS(middleware.DefaultCORS()),
		middleware.Recovery(),
		middleware.Logger(),
		middleware.ErrorHandler(middleware.DefaultErrorHandler()),
	)

	app.SetVersion("v1").
		SetOnPanic(func(c *zentrox.Context, v any) {
			log.Printf("panic: %v", v)
		}).
		SetPrintRoutes(true)

	app.GET("/", func(c *zentrox.Context) {
		_ = c.String(http.StatusOK, "zentrox up!")
	})

	app.GET("/ping", func(c *zentrox.Context) {
		_ = c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	app.GET("/:id", func(c *zentrox.Context) {
		id := html.EscapeString(c.Param("id"))
		txt := handleLogic(c, id, "req-123")
		_ = c.JSON(http.StatusOK, map[string]string{"result": txt})
	})

	app.GET("/fail", func(c *zentrox.Context) {
		_ = c.Fail(http.StatusBadRequest, "invalid argument", map[string]any{"field": "q"})
	})

	app.GET("/panic", func(c *zentrox.Context) {
		panic("boom")
	})

	app.Static("/assets", zentrox.StaticOptions{
		Dir:           "./public",
		Index:         "index.html",
		MaxAge:        24 * time.Hour,
		UseStrongETag: false,
		AllowedExt:    []string{".html", ".css", ".js", ".png", ".jpg", ".svg", ".ico"},
	})

	app.POST("/upload", func(ctx *zentrox.Context) {
		saved, err := ctx.SaveUploadedFile("file", "./uploads", zentrox.UploadOptions{
			MaxMemory:          10 << 20,
			AllowedExt:         []string{".png", ".jpg", ".jpeg", ".pdf"},
			Sanitize:           true,
			GenerateUniqueName: true,
			Overwrite:          false,
		})
		if err != nil {
			_ = ctx.Fail(http.StatusBadRequest, "upload error", html.EscapeString(err.Error()))
			return
		}
		_ = ctx.JSON(http.StatusOK, map[string]any{"saved": saved})
	})

	log.Println("listening on :8000")
	_ = app.Run(":8000")
}
