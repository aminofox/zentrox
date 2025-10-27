package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aminofox/zentrox"
	"github.com/aminofox/zentrox/middleware"
)

func main() {
	app := zentrox.NewApp()

	// Standardize errors & panics
	app.Plug(middleware.ErrorHandler(middleware.DefaultErrorHandler()))

	app.GET("/", func(c *zentrox.Context) {
		c.String(http.StatusOK, "zentrox up!")
	})

	app.GET("/fail", func(c *zentrox.Context) {
		c.Fail(http.StatusBadRequest, "invalid argument", map[string]any{"field": "q"})
	})

	app.GET("/panic", func(c *zentrox.Context) {
		panic("boom")
	})

	app.Health("/healthz", "/readyz", func() bool { return true })

	// Create & start server
	srv, err := app.Start(&zentrox.ServerConfig{
		Addr:              ":8000",
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	})
	if err != nil {
		log.Fatalf("failed to start server: %v", err)
	}

	log.Println("listening on :8000")

	// Wait for OS signal (Ctrl+C, docker stop, k8s SIGTERM, ...)
	// When a signal arrives, ctx is canceled.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	<-ctx.Done() // block here until signal

	log.Println("shutdown signal received, draining connections...")

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := app.Shutdown(shutdownCtx, srv); err != nil {
		log.Printf("graceful shutdown error: %v", err)
	} else {
		log.Println("server shut down cleanly")
	}
}
