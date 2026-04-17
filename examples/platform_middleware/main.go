package main

import (
	"log"
	"net/http"
	"time"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
)

func main() {
	app := zentrox.NewApp()

	app.Plug(
		middleware.RequestID(middleware.DefaultRequestID()),
		middleware.RateLimit(middleware.RateLimitConfig{
			Rate:  10,
			Burst: 20,
		}),
		middleware.Timeout(200*time.Millisecond),
	)

	app.GET("/", func(c *zentrox.Context) {
		c.JSON(http.StatusOK, map[string]any{
			"status":     "ok",
			"request_id": c.RequestID(),
		})
	})

	app.GET("/slow", func(c *zentrox.Context) {
		select {
		case <-time.After(500 * time.Millisecond):
			c.String(http.StatusOK, "finished")
		case <-c.Done():
			return
		}
	})

	log.Println("listening on :8000")
	_ = app.Run(":8000")
}
