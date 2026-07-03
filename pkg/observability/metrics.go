package observability

import (
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type Collector struct {
	mu               sync.RWMutex
	startedAt        time.Time
	requestTotal     int64
	errorTotal       int64
	statusCounts     map[string]int64
	routeCounts      map[string]int64
	lastLatencyMS    float64
	externalCounters map[string]int64
}

type Snapshot struct {
	StartedAt        time.Time        `json:"started_at"`
	RequestTotal     int64            `json:"request_total"`
	ErrorTotal       int64            `json:"error_total"`
	StatusCounts     map[string]int64 `json:"status_counts"`
	RouteCounts      map[string]int64 `json:"route_counts"`
	LastLatencyMS    float64          `json:"last_latency_ms"`
	ExternalCounters map[string]int64 `json:"external_counters"`
}

func NewCollector() *Collector {
	return &Collector{
		startedAt:        time.Now().UTC(),
		statusCounts:     make(map[string]int64),
		routeCounts:      make(map[string]int64),
		externalCounters: make(map[string]int64),
	}
}

func Middleware(c *Collector) gin.HandlerFunc {
	if c == nil {
		c = NewCollector()
	}
	return func(ctx *gin.Context) {
		start := time.Now()
		ctx.Next()
		c.RecordRequest(ctx.FullPath(), ctx.Writer.Status(), time.Since(start))
	}
}

func (c *Collector) RecordRequest(route string, status int, latency time.Duration) {
	if c == nil {
		return
	}
	if route == "" {
		route = "unmatched"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requestTotal++
	if status >= 400 {
		c.errorTotal++
	}
	c.statusCounts[strconv.Itoa(status)]++
	c.routeCounts[route]++
	c.lastLatencyMS = float64(latency.Microseconds()) / 1000
}

func (c *Collector) RecordExternal(name string, success bool) {
	if c == nil || name == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.externalCounters[name+".total"]++
	if success {
		c.externalCounters[name+".success"]++
	} else {
		c.externalCounters[name+".error"]++
	}
}

func (c *Collector) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return Snapshot{
		StartedAt:        c.startedAt,
		RequestTotal:     c.requestTotal,
		ErrorTotal:       c.errorTotal,
		StatusCounts:     copyInt64Map(c.statusCounts),
		RouteCounts:      copyInt64Map(c.routeCounts),
		LastLatencyMS:    c.lastLatencyMS,
		ExternalCounters: copyInt64Map(c.externalCounters),
	}
}

func copyInt64Map(values map[string]int64) map[string]int64 {
	clone := make(map[string]int64, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}
