package telemetry

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

const contentTypeTextUTF8 = "text/plain; charset=utf-8"

// Counter is a simple, mutex-protected counter.
type Counter struct {
	mu  sync.Mutex
	val uint64
}

func (c *Counter) Add(n uint64) {
	c.mu.Lock()
	c.val += n
	c.mu.Unlock()
}

func (c *Counter) Load() uint64 {
	c.mu.Lock()
	v := c.val
	c.mu.Unlock()
	return v
}

// Histogram with fixed buckets (milliseconds) and last bucket as +Inf.
type Histogram struct {
	mu      sync.Mutex
	buckets []float64 // upper bounds in ms
	counts  []uint64
	sum     float64
	count   uint64
}

func NewHistogram(boundsMS []float64) *Histogram {
	if len(boundsMS) == 0 {
		boundsMS = []float64{10, 25, 50, 100, 250, 500, 1000, 2000}
	}
	return &Histogram{
		buckets: boundsMS,
		counts:  make([]uint64, len(boundsMS)),
	}
}

func (h *Histogram) Observe(ms float64) {
	h.mu.Lock()
	h.sum += ms
	h.count++
	// place in first matching bucket; last bucket doubles as +Inf
	placed := false
	for i, ub := range h.buckets {
		if ms <= ub {
			h.counts[i]++
			placed = true
			break
		}
	}
	if !placed && len(h.counts) > 0 {
		h.counts[len(h.counts)-1]++
	}
	h.mu.Unlock()
}

func (h *Histogram) Snapshot() (bounds []float64, counts []uint64, sum float64, count uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	bounds = append([]float64(nil), h.buckets...)
	counts = append([]uint64(nil), h.counts...)
	sum = h.sum
	count = h.count
	return
}

// Registry contains a few server metrics.
type Registry struct {
	Requests Counter
	Latency  *Histogram
	StartAt  time.Time
}

func NewRegistry() *Registry {
	return &Registry{
		Latency: NewHistogram(nil),
		StartAt: time.Now(),
	}
}

// MetricsHandler renders a plain-text snapshot (prometheus-like style).
func MetricsHandler(reg *Registry) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentTypeTextUTF8)
		fmt.Fprintf(w, "uptime_seconds %d\n", int(time.Since(reg.StartAt).Seconds()))
		fmt.Fprintf(w, "requests_total %d\n", reg.Requests.Load())

		bounds, counts, sum, count := reg.Latency.Snapshot()
		fmt.Fprintf(w, "latency_count %d\n", count)
		fmt.Fprintf(w, "latency_sum_ms %.3f\n", sum)
		for i, ub := range bounds {
			fmt.Fprintf(w, "latency_bucket_ms{le=\"%.0f\"} %d\n", ub, counts[i])
		}
	})
}
