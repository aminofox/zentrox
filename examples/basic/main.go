package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
)

func handleLogic(ctx context.Context, param, requestID string) string {
	return fmt.Sprintf(`[handleLogic] with param %s and requestID %s`, param, requestID)
}

func main() {
	app := zentrox.NewApp()

	// Use ErrorHandler to standardize errors & panics
	app.Plug(
		middleware.CORS(middleware.CORSConfig{
			AllowOrigins:     []string{"http://localhost:5173", "*"},
			AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowHeaders:     []string{"Content-Type", "Authorization"},
			AllowCredentials: false,
			MaxAge:           3600,
		}),
		middleware.BodyLimit(2<<20), // 2 MiB
		middleware.ErrorHandler(middleware.DefaultErrorHandler()),
		middleware.RequestID(middleware.DefaultRequestID()),
		middleware.AccessLog(middleware.DefaultAccessLog()),
	)

	app.SetVersion("v1").
		SetOnPanic(func(c *zentrox.Context, v any) {
			// Send to crash reporter, metrics, etc.
			log.Printf("panic on %s %s (rid=%s): %v", c.Request.Method, c.Request.URL.Path, c.RequestID(), v)
		}).
		SetOnResponse(func(c *zentrox.Context, status int, dur time.Duration) {
			log.Printf("response on %s %s (rid=%s): status %v, time: %v", c.Request.Method, c.Request.URL.Path, c.RequestID(), status, dur)
		}).
		SetOnRequest(func(c *zentrox.Context) {
			log.Printf("request on %s %s ", c.Request.Method, c.Request.URL.Path)
		}).
		SetPrintRoutes(true)

	app.OnGet("/", func(c *zentrox.Context) {
		c.SendText(http.StatusOK, `zentrox up!`+c.RequestID())
	})

	app.OnGet("/ping", func(c *zentrox.Context) {
		c.SendJSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	app.OnGet(":id", func(c *zentrox.Context) {
		txt := handleLogic(c, c.Param("id"), c.RequestID())
		c.SendText(http.StatusOK, txt)
	})

	// Example: standardized error payload
	app.OnGet("/fail", func(c *zentrox.Context) {
		c.Fail(http.StatusBadRequest, "invalid argument", map[string]any{"field": "q"})
	})

	// Example: panic -> 500 standardized JSON
	app.OnGet("/panic", func(c *zentrox.Context) {
		panic("boom")
	})

	app.Static("/assets", zentrox.StaticOptions{
		Dir:           "./public",
		Index:         "index.html",
		MaxAge:        24 * time.Hour,
		UseStrongETag: false, // set true if you prefer stronger cache key (requires hashing file)
		AllowedExt:    []string{".html", ".css", ".js", ".png", ".jpg", ".svg", ".ico"},
	})

	app.OnPost("/upload", func(ctx *zentrox.Context) {
		// Body size protection is strongly recommended (Milestone 9 BodyLimit)
		// app.Plug(middleware.BodyLimit(20 << 20)) // 20 MiB
		saved, err := ctx.SaveUploadedFile("file", "./uploads", zentrox.UploadOptions{
			MaxMemory:          10 << 20,
			AllowedExt:         []string{".png", ".jpg", ".jpeg", ".pdf"},
			Sanitize:           true,
			GenerateUniqueName: true,
			Overwrite:          false,
		})
		if err != nil {
			ctx.Fail(http.StatusBadRequest, "upload error", err.Error())
			return
		}
		ctx.SendJSON(http.StatusOK, map[string]any{"saved": saved})
	})

	log.Println("listening on :8000")
	_ = app.Run(":8000")
}
