package main

import (
	"log"
	"net/http"
	"net/http/pprof"
	"time"

	"github.com/aminofox/zentrox/v2"
	"github.com/aminofox/zentrox/v2/middleware"
)

func main() {
	app := zentrox.NewApp()

	// Full production middleware stack
	app.Plug(
		middleware.Recovery(),
		middleware.RequestID(middleware.RequestIDConfig{}),
		middleware.Logger(),
		middleware.CORS(middleware.CORSConfig{
			AllowOrigins: []string{"*"},
			AllowMethods: []string{"GET", "POST", "PUT", "DELETE"},
			AllowHeaders: []string{"Origin", "Content-Type", "Accept", "Authorization"},
		}),
	)

	// Sample endpoint
	app.GET("/api/test", func(c *zentrox.Context) {
		_ = c.JSON(http.StatusOK, map[string]any{
			"status": "ok",
			"time":   time.Now().Unix(),
		})
	})

	// Add pprof routes for leak detection
	app.GET("/debug/pprof/", func(c *zentrox.Context) {
		pprof.Index(c.Writer, c.Request)
	})
	app.GET("/debug/pprof/heap", func(c *zentrox.Context) {
		pprof.Handler("heap").ServeHTTP(c.Writer, c.Request)
	})
	app.GET("/debug/pprof/goroutine", func(c *zentrox.Context) {
		pprof.Handler("goroutine").ServeHTTP(c.Writer, c.Request)
	})
	app.GET("/debug/pprof/allocs", func(c *zentrox.Context) {
		pprof.Handler("allocs").ServeHTTP(c.Writer, c.Request)
	})

	log.Println("Soak test server starting on :8080")
	log.Println("Run a load generator like bombardier for several hours and check /debug/pprof/heap")
	if err := app.Run(":8080"); err != nil {
		log.Fatal(err)
	}
}
