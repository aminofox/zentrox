# Zentrox Production Guide

This guide outlines best practices for deploying Zentrox in production environments, such as Kubernetes or Docker Swarm.

## 1. Graceful Shutdown

Always use `app.RunGracefully()` or standard `http.Server.Shutdown(ctx)` instead of `app.Run()` to ensure that active requests finish before the server exits.

## 2. Proxies and Real IP

If you deploy behind an Ingress controller or Load Balancer (like Nginx, AWS ALB), ensure you configure it to pass `X-Forwarded-For` correctly. Use `c.RealIP()` which relies on the `realIP` logic, but **only trust** `X-Forwarded-For` from known upstream proxies to avoid IP spoofing.

## 3. Liveness and Readiness Probes (Kubernetes)

Configure separate endpoints for Kubernetes probes. They should ideally not write logs to keep observability clean.

```go
app.GET("/healthz", func(c *zentrox.Context) {
    c.SendStatus(200)
})
```

## 4. Timeouts

Do not rely solely on Zentrox's middleware timeouts. Configure global timeouts on `http.Server` to protect against slowloris attacks:

```go
server := &http.Server{
    Addr:         ":8080",
    ReadTimeout:  10 * time.Second,
    WriteTimeout: 30 * time.Second,
    IdleTimeout:  120 * time.Second,
}
```

## 5. Metrics and Tracing

In a distributed environment, use OpenTelemetry (`middleware/otel.go`) or Prometheus to trace requests across microservices.
