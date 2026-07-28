# Zentrox Benchmarks

This directory contains reproducible benchmarks to compare Zentrox against other popular Go frameworks like Gin, Chi, Fiber, and `net/http`.

To ensure fair comparisons, these benchmarks are run with:
- The exact same payload and middleware stack (Logger, Recovery, CORS, JSON response).
- Consistent conditions (CPU cores, Go version, concurrency levels).
- Comprehensive latency metrics (p50, p95, p99, p99.9).

## Running Benchmarks

We use standard Go benchmarking tools alongside `bombardier` or `wrk` for HTTP level load testing.

```bash
# Run router micro-benchmarks
go test -bench=. -benchmem ./...

# To run HTTP level load tests:
# 1. Start the benchmark server
go run ./benchmarks/server.go -framework zentrox

# 2. Run bombardier in another terminal
bombardier -c 125 -n 1000000 http://localhost:8080/api/users/123
```

Detailed reports with p50, p95, and p99 percentiles will be updated here as we execute long-running soak tests on production hardware.
