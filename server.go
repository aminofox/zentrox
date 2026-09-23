package zentrox

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"time"
)

// ServerConfig controls the underlying http.Server configuration.
type ServerConfig struct {
	Addr              string
	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int
	ErrorLog          *log.Logger
	BaseContext       func(net.Listener) context.Context
}

// Run starts a blocking server on addr.
func (a *App) Run(addr string) error {
	cfg := &ServerConfig{Addr: addr}
	srv := a.buildServer(cfg)
	return srv.ListenAndServe()
}

func (a *App) buildServer(cfg *ServerConfig) *http.Server {
	a.freeze()

	c := ServerConfig{
		Addr:              ":8000",
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}
	if cfg != nil {
		if cfg.Addr != "" {
			c.Addr = cfg.Addr
		}
		if cfg.ReadHeaderTimeout > 0 {
			c.ReadHeaderTimeout = cfg.ReadHeaderTimeout
		}
		if cfg.ReadTimeout > 0 {
			c.ReadTimeout = cfg.ReadTimeout
		}
		if cfg.WriteTimeout > 0 {
			c.WriteTimeout = cfg.WriteTimeout
		}
		if cfg.IdleTimeout > 0 {
			c.IdleTimeout = cfg.IdleTimeout
		}
		if cfg.MaxHeaderBytes > 0 {
			c.MaxHeaderBytes = cfg.MaxHeaderBytes
		}
		if cfg.ErrorLog != nil {
			c.ErrorLog = cfg.ErrorLog
		}
		if cfg.BaseContext != nil {
			c.BaseContext = cfg.BaseContext
		}
	}
	if c.ErrorLog == nil {
		c.ErrorLog = log.New(os.Stderr, "zentrox/http: ", log.LstdFlags)
	}

	srv := &http.Server{
		Addr:              c.Addr,
		Handler:           a,
		ReadHeaderTimeout: c.ReadHeaderTimeout,
		ReadTimeout:       c.ReadTimeout,
		WriteTimeout:      c.WriteTimeout,
		IdleTimeout:       c.IdleTimeout,
		MaxHeaderBytes:    c.MaxHeaderBytes,
		ErrorLog:          c.ErrorLog,
	}
	if c.BaseContext != nil {
		srv.BaseContext = c.BaseContext
	}
	if a.printRoutes {
		a.PrintRoutes(os.Stdout)
	}
	return srv
}

// Start starts the server asynchronously and returns *http.Server.
func (a *App) Start(cfg *ServerConfig) (*http.Server, error) {
	srv := a.buildServer(cfg)
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			srv.ErrorLog.Printf("listen error: %v", err)
		}
	}()
	return srv, nil
}

// StartTLS starts a TLS server asynchronously and returns *http.Server.
func (a *App) StartTLS(cfg *ServerConfig, certFile, keyFile string) (*http.Server, error) {
	srv := a.buildServer(cfg)
	go func() {
		if err := srv.ListenAndServeTLS(certFile, keyFile); err != nil && err != http.ErrServerClosed {
			srv.ErrorLog.Printf("listen (tls) error: %v", err)
		}
	}()
	return srv, nil
}

// Shutdown requests graceful server shutdown.
func (a *App) Shutdown(ctx context.Context, srv *http.Server) error {
	return srv.Shutdown(ctx)
}

// Health mounts standard liveness and readiness endpoints.
func (a *App) Health(livenessPath, readinessPath string, ready func() bool) {
	if livenessPath != "" {
		a.GET(livenessPath, func(c *Context) { _ = c.String(http.StatusOK, "ok") })
	}
	if readinessPath != "" && ready != nil {
		a.GET(readinessPath, func(c *Context) {
			if ready() {
				_ = c.String(http.StatusOK, "ready")
				return
			}
			_ = c.String(http.StatusServiceUnavailable, "not ready")
		})
	}
}
