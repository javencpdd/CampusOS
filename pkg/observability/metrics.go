package observability

import (
	"errors"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	RouteOperationContextKey = "route_operation"
	defaultMaximumSeries     = 512
)

type MetricType string

const (
	MetricCounter   MetricType = "counter"
	MetricGauge     MetricType = "gauge"
	MetricHistogram MetricType = "histogram"
)

type MetricDescriptor struct {
	Name          string     `json:"name"`
	Help          string     `json:"help"`
	Type          MetricType `json:"type"`
	Unit          string     `json:"unit"`
	LabelNames    []string   `json:"label_names,omitempty"`
	Buckets       []float64  `json:"buckets,omitempty"`
	MaximumSeries int        `json:"maximum_series"`
}

type Labels map[string]string

type Meter interface {
	AddCounter(string, Labels, float64) error
	SetGauge(string, Labels, float64) error
	Observe(string, Labels, float64) error
}

var metricDescriptors = []MetricDescriptor{
	{Name: "campusos_http_requests_total", Help: "Total CampusOS HTTP requests.", Type: MetricCounter, Unit: "requests", LabelNames: []string{"method", "operation", "status_class"}, MaximumSeries: defaultMaximumSeries},
	{Name: "campusos_http_request_duration_seconds", Help: "CampusOS HTTP request duration.", Type: MetricHistogram, Unit: "seconds", LabelNames: []string{"method", "operation", "status_class"}, Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}, MaximumSeries: defaultMaximumSeries},
	{Name: "campusos_http_in_flight", Help: "Current CampusOS HTTP requests in flight.", Type: MetricGauge, Unit: "requests", MaximumSeries: 1},
	{Name: "campusos_http_response_size_bytes", Help: "CampusOS HTTP response body size.", Type: MetricHistogram, Unit: "bytes", LabelNames: []string{"method", "operation", "status_class"}, Buckets: []float64{256, 1024, 4096, 16384, 65536, 262144, 1048576}, MaximumSeries: defaultMaximumSeries},
	{Name: "campusos_http_panics_total", Help: "Total recovered CampusOS HTTP panics.", Type: MetricCounter, Unit: "panics", LabelNames: []string{"operation"}, MaximumSeries: defaultMaximumSeries},
	{Name: "campusos_external_requests_total", Help: "Total bounded external integration requests.", Type: MetricCounter, Unit: "requests", LabelNames: []string{"integration", "result"}, MaximumSeries: 128},
	{Name: "campusos_module_operations_total", Help: "Total bounded module operations.", Type: MetricCounter, Unit: "operations", LabelNames: []string{"module", "operation", "result"}, MaximumSeries: defaultMaximumSeries},
	{Name: "campusos_module_operation_duration_seconds", Help: "CampusOS module operation duration.", Type: MetricHistogram, Unit: "seconds", LabelNames: []string{"module", "operation", "result"}, Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}, MaximumSeries: defaultMaximumSeries},
	{Name: "campusos_db_pool_connections", Help: "Current PostgreSQL pool connections.", Type: MetricGauge, Unit: "connections", LabelNames: []string{"state"}, MaximumSeries: 8},
	{Name: "campusos_db_pool_acquire_total", Help: "Cumulative PostgreSQL pool acquires.", Type: MetricCounter, Unit: "acquires", MaximumSeries: 1},
	{Name: "campusos_db_pool_acquire_duration_seconds_total", Help: "Cumulative PostgreSQL pool acquire duration.", Type: MetricCounter, Unit: "seconds", MaximumSeries: 1},
	{Name: "campusos_db_pool_acquire_errors_total", Help: "Cumulative PostgreSQL pool acquire failures.", Type: MetricCounter, Unit: "errors", LabelNames: []string{"kind"}, MaximumSeries: 4},
	{Name: "campusos_db_pool_empty_wait_seconds_total", Help: "Cumulative wait while the PostgreSQL pool was empty.", Type: MetricCounter, Unit: "seconds", MaximumSeries: 1},
	{Name: "campusos_reliability_operations_total", Help: "Total durable-event worker operations.", Type: MetricCounter, Unit: "operations", LabelNames: []string{"operation", "result"}, MaximumSeries: 64},
	{Name: "campusos_reliability_queue_events", Help: "Current durable events by queue status.", Type: MetricGauge, Unit: "events", LabelNames: []string{"status"}, MaximumSeries: 8},
	{Name: "campusos_reliability_oldest_pending_age_seconds", Help: "Age of the oldest pending or retryable durable event.", Type: MetricGauge, Unit: "seconds", MaximumSeries: 1},
	{Name: "campusos_reliability_consumer_duration_seconds", Help: "Durable-event consumer execution duration.", Type: MetricHistogram, Unit: "seconds", LabelNames: []string{"consumer", "result"}, Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}, MaximumSeries: 256},
	{Name: "campusos_email_delivery_total", Help: "Total email delivery outcomes.", Type: MetricCounter, Unit: "deliveries", LabelNames: []string{"provider", "result"}, MaximumSeries: 32},
	{Name: "campusos_email_delivery_duration_seconds", Help: "Email provider delivery duration.", Type: MetricHistogram, Unit: "seconds", LabelNames: []string{"provider", "result"}, Buckets: []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}, MaximumSeries: 32},
	{Name: "campusos_identity_challenges_total", Help: "Total Identity challenge outcomes.", Type: MetricCounter, Unit: "challenges", LabelNames: []string{"operation", "result"}, MaximumSeries: 32},
	{Name: "campusos_identity_mfa_total", Help: "Total Identity multi-factor authentication outcomes.", Type: MetricCounter, Unit: "operations", LabelNames: []string{"operation", "result"}, MaximumSeries: 48},
	{Name: "campusos_identity_sessions_total", Help: "Total Identity session outcomes.", Type: MetricCounter, Unit: "sessions", LabelNames: []string{"operation", "result"}, MaximumSeries: 48},
	{Name: "campusos_runtime_goroutines", Help: "Current Go runtime goroutine count.", Type: MetricGauge, Unit: "goroutines", MaximumSeries: 1},
	{Name: "campusos_runtime_heap_alloc_bytes", Help: "Current Go runtime allocated heap bytes.", Type: MetricGauge, Unit: "bytes", MaximumSeries: 1},
}

var metricCatalog = mustMetricCatalog(metricDescriptors)

type seriesState struct {
	descriptor MetricDescriptor
	labels     Labels
	value      float64
	count      uint64
	sum        float64
	buckets    []uint64
}

type Collector struct {
	mu               sync.RWMutex
	startedAt        time.Time
	requestTotal     int64
	errorTotal       int64
	inFlight         int64
	statusCounts     map[string]int64
	routeCounts      map[string]int64
	lastLatencyMS    float64
	externalCounters map[string]int64
	series           map[string]*seriesState
	seriesCounts     map[string]int
	databaseStats    func() DatabaseSnapshot
}

type Snapshot struct {
	StartedAt        time.Time         `json:"started_at"`
	RequestTotal     int64             `json:"request_total"`
	ErrorTotal       int64             `json:"error_total"`
	InFlight         int64             `json:"in_flight"`
	StatusCounts     map[string]int64  `json:"status_counts"`
	RouteCounts      map[string]int64  `json:"route_counts"`
	LastLatencyMS    float64           `json:"last_latency_ms"`
	ExternalCounters map[string]int64  `json:"external_counters"`
	Database         *DatabaseSnapshot `json:"database,omitempty"`
	Runtime          RuntimeSnapshot   `json:"runtime"`
	Metrics          []MetricSnapshot  `json:"metrics,omitempty"`
}

type MetricSnapshot struct {
	Name    string           `json:"name"`
	Type    MetricType       `json:"type"`
	Unit    string           `json:"unit"`
	Labels  Labels           `json:"labels,omitempty"`
	Value   float64          `json:"value,omitempty"`
	Count   uint64           `json:"count,omitempty"`
	Sum     float64          `json:"sum,omitempty"`
	Buckets []BucketSnapshot `json:"buckets,omitempty"`
}

type BucketSnapshot struct {
	UpperBound float64 `json:"upper_bound"`
	Count      uint64  `json:"count"`
}

type DatabaseSnapshot struct {
	AcquiredConnections     int32   `json:"acquired_connections"`
	IdleConnections         int32   `json:"idle_connections"`
	TotalConnections        int32   `json:"total_connections"`
	MaximumConnections      int32   `json:"maximum_connections"`
	AcquireCount            int64   `json:"acquire_count"`
	AcquireDurationSeconds  float64 `json:"acquire_duration_seconds"`
	CanceledAcquireCount    int64   `json:"canceled_acquire_count"`
	EmptyAcquireCount       int64   `json:"empty_acquire_count"`
	EmptyAcquireWaitSeconds float64 `json:"empty_acquire_wait_seconds"`
}

// RuntimeSnapshot contains only process-level counters. It intentionally does
// not expose command-line arguments, environment variables, or profile data.
type RuntimeSnapshot struct {
	Goroutines     int    `json:"goroutines"`
	HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
	HeapObjects    uint64 `json:"heap_objects"`
}

func NewCollector() *Collector {
	return &Collector{
		startedAt:        time.Now().UTC(),
		statusCounts:     make(map[string]int64),
		routeCounts:      make(map[string]int64),
		externalCounters: make(map[string]int64),
		series:           make(map[string]*seriesState),
		seriesCounts:     make(map[string]int),
	}
}

func MetricCatalog() []MetricDescriptor {
	items := make([]MetricDescriptor, 0, len(metricDescriptors))
	for _, item := range metricDescriptors {
		copy := item
		copy.LabelNames = append([]string(nil), item.LabelNames...)
		copy.Buckets = append([]float64(nil), item.Buckets...)
		items = append(items, copy)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items
}

func ValidateMetricCatalog(items []MetricDescriptor) error {
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if !validMetricName(item.Name) {
			return fmt.Errorf("invalid metric name %q", item.Name)
		}
		if strings.TrimSpace(item.Help) == "" || strings.TrimSpace(item.Unit) == "" {
			return fmt.Errorf("metric %s requires help and unit", item.Name)
		}
		if item.Type != MetricCounter && item.Type != MetricGauge && item.Type != MetricHistogram {
			return fmt.Errorf("metric %s has invalid type %q", item.Name, item.Type)
		}
		if item.MaximumSeries < 1 || item.MaximumSeries > 4096 {
			return fmt.Errorf("metric %s has invalid maximum series %d", item.Name, item.MaximumSeries)
		}
		if _, exists := seen[item.Name]; exists {
			return fmt.Errorf("duplicate metric name %s", item.Name)
		}
		seen[item.Name] = struct{}{}
		labelSeen := make(map[string]struct{}, len(item.LabelNames))
		for _, label := range item.LabelNames {
			if !validLabelName(label) {
				return fmt.Errorf("metric %s has invalid label %q", item.Name, label)
			}
			if _, exists := labelSeen[label]; exists {
				return fmt.Errorf("metric %s has duplicate label %q", item.Name, label)
			}
			labelSeen[label] = struct{}{}
		}
		previous := -1.0
		for _, bucket := range item.Buckets {
			if item.Type != MetricHistogram || bucket <= previous || math.IsInf(bucket, 0) || math.IsNaN(bucket) {
				return fmt.Errorf("metric %s has invalid histogram buckets", item.Name)
			}
			previous = bucket
		}
	}
	return nil
}

func Middleware(collector *Collector) gin.HandlerFunc {
	if collector == nil {
		collector = NewCollector()
	}
	return func(ctx *gin.Context) {
		start := time.Now()
		collector.beginRequest()
		defer func() {
			size := ctx.Writer.Size()
			if size < 0 {
				size = 0
			}
			operation := strings.TrimSpace(ctx.GetString(RouteOperationContextKey))
			if operation == "" {
				operation = strings.TrimSpace(ctx.FullPath())
			}
			collector.endRequest(operation, ctx.Request.Method, ctx.Writer.Status(), time.Since(start), size)
		}()
		ctx.Next()
	}
}

func (c *Collector) RecordRequest(route string, status int, latency time.Duration) {
	c.recordRequest(route, "UNKNOWN", status, latency, 0)
}

func (c *Collector) RecordPanic(operation string) {
	if c == nil {
		return
	}
	operation = normalizedOperation(operation)
	_ = c.AddCounter("campusos_http_panics_total", Labels{"operation": operation}, 1)
}

func (c *Collector) RecordExternal(name string, success bool) {
	if c == nil || strings.TrimSpace(name) == "" {
		return
	}
	name = strings.TrimSpace(name)
	result := "error"
	if success {
		result = "success"
	}
	c.mu.Lock()
	c.externalCounters[name+".total"]++
	c.externalCounters[name+"."+result]++
	c.mu.Unlock()
	_ = c.AddCounter("campusos_external_requests_total", Labels{"integration": name, "result": result}, 1)
}

func (c *Collector) ForModule(module string) Meter {
	return &moduleMeter{collector: c, module: strings.TrimSpace(module)}
}

func (c *Collector) AddCounter(name string, labels Labels, delta float64) error {
	return c.recordMetric(name, MetricCounter, labels, delta, false)
}

func (c *Collector) SetGauge(name string, labels Labels, value float64) error {
	return c.recordMetric(name, MetricGauge, labels, value, true)
}

func (c *Collector) Observe(name string, labels Labels, value float64) error {
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return errors.New("metric observation must be a finite non-negative value")
	}
	descriptor, ok := metricCatalog[name]
	if !ok || descriptor.Type != MetricHistogram {
		return fmt.Errorf("metric %q is not a registered histogram", name)
	}
	key, safeLabels, err := metricSeriesKey(descriptor, labels)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state, err := c.ensureSeriesLocked(key, descriptor, safeLabels)
	if err != nil {
		return err
	}
	state.count++
	state.sum += value
	for index, upper := range descriptor.Buckets {
		if value <= upper {
			state.buckets[index]++
		}
	}
	return nil
}

func (c *Collector) SetDatabaseStatsProvider(provider func() DatabaseSnapshot) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.databaseStats = provider
	c.mu.Unlock()
}

func (c *Collector) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{}
	}
	c.mu.RLock()
	snapshot := Snapshot{
		StartedAt:        c.startedAt,
		RequestTotal:     c.requestTotal,
		ErrorTotal:       c.errorTotal,
		InFlight:         c.inFlight,
		StatusCounts:     copyInt64Map(c.statusCounts),
		RouteCounts:      copyInt64Map(c.routeCounts),
		LastLatencyMS:    c.lastLatencyMS,
		ExternalCounters: copyInt64Map(c.externalCounters),
		Runtime:          runtimeSnapshot(),
		Metrics:          metricSnapshots(c.series),
	}
	provider := c.databaseStats
	c.mu.RUnlock()
	if provider != nil {
		database := provider()
		snapshot.Database = &database
	}
	return snapshot
}

func (c *Collector) PrometheusText() string {
	snapshot := c.Snapshot()
	series := append([]MetricSnapshot(nil), snapshot.Metrics...)
	if snapshot.Database != nil {
		series = append(series, databaseMetricSnapshots(*snapshot.Database)...)
	}
	series = append(series, runtimeMetricSnapshots(snapshot.Runtime)...)
	byName := make(map[string][]MetricSnapshot)
	for _, item := range series {
		byName[item.Name] = append(byName[item.Name], item)
	}
	var out strings.Builder
	for _, descriptor := range MetricCatalog() {
		fmt.Fprintf(&out, "# HELP %s %s\n", descriptor.Name, descriptor.Help)
		fmt.Fprintf(&out, "# TYPE %s %s\n", descriptor.Name, descriptor.Type)
		items := byName[descriptor.Name]
		sort.Slice(items, func(i, j int) bool { return labelsKey(items[i].Labels) < labelsKey(items[j].Labels) })
		for _, item := range items {
			writePrometheusSeries(&out, descriptor, item)
		}
	}
	return out.String()
}

func (c *Collector) beginRequest() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.inFlight++
	c.mu.Unlock()
	_ = c.adjustGauge("campusos_http_in_flight", nil, 1)
}

func (c *Collector) endRequest(operation, method string, status int, latency time.Duration, size int) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if c.inFlight > 0 {
		c.inFlight--
	}
	c.mu.Unlock()
	_ = c.adjustGauge("campusos_http_in_flight", nil, -1)
	c.recordRequest(operation, method, status, latency, size)
}

func (c *Collector) recordRequest(operation, method string, status int, latency time.Duration, size int) {
	if c == nil {
		return
	}
	operation = normalizedOperation(operation)
	method = strings.ToUpper(strings.TrimSpace(method))
	if method == "" {
		method = "UNKNOWN"
	}
	statusClass := strconv.Itoa(status/100) + "xx"
	c.mu.Lock()
	c.requestTotal++
	if status >= 400 {
		c.errorTotal++
	}
	c.statusCounts[strconv.Itoa(status)]++
	c.routeCounts[operation]++
	c.lastLatencyMS = float64(latency.Microseconds()) / 1000
	c.mu.Unlock()
	labels := Labels{"method": method, "operation": operation, "status_class": statusClass}
	_ = c.AddCounter("campusos_http_requests_total", labels, 1)
	_ = c.Observe("campusos_http_request_duration_seconds", labels, latency.Seconds())
	_ = c.Observe("campusos_http_response_size_bytes", labels, float64(size))
}

func (c *Collector) recordMetric(name string, want MetricType, labels Labels, value float64, replace bool) error {
	if c == nil {
		return errors.New("metric collector is unavailable")
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || (want == MetricCounter && value < 0) {
		return errors.New("metric value must be finite and counters cannot decrease")
	}
	descriptor, ok := metricCatalog[name]
	if !ok || descriptor.Type != want {
		return fmt.Errorf("metric %q is not a registered %s", name, want)
	}
	key, safeLabels, err := metricSeriesKey(descriptor, labels)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state, err := c.ensureSeriesLocked(key, descriptor, safeLabels)
	if err != nil {
		return err
	}
	if replace {
		state.value = value
	} else {
		state.value += value
	}
	return nil
}

func (c *Collector) adjustGauge(name string, labels Labels, delta float64) error {
	descriptor, ok := metricCatalog[name]
	if !ok || descriptor.Type != MetricGauge {
		return fmt.Errorf("metric %q is not a registered gauge", name)
	}
	key, safeLabels, err := metricSeriesKey(descriptor, labels)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	state, err := c.ensureSeriesLocked(key, descriptor, safeLabels)
	if err != nil {
		return err
	}
	state.value += delta
	if state.value < 0 {
		state.value = 0
	}
	return nil
}

func (c *Collector) ensureSeriesLocked(key string, descriptor MetricDescriptor, labels Labels) (*seriesState, error) {
	if state := c.series[key]; state != nil {
		return state, nil
	}
	if c.seriesCounts[descriptor.Name] >= descriptor.MaximumSeries {
		return nil, fmt.Errorf("metric %s reached its bounded series limit", descriptor.Name)
	}
	state := &seriesState{descriptor: descriptor, labels: labels, buckets: make([]uint64, len(descriptor.Buckets))}
	c.series[key] = state
	c.seriesCounts[descriptor.Name]++
	return state, nil
}

type moduleMeter struct {
	collector *Collector
	module    string
}

func (m *moduleMeter) AddCounter(name string, labels Labels, delta float64) error {
	return m.collector.AddCounter(name, m.withModule(labels), delta)
}

func (m *moduleMeter) SetGauge(name string, labels Labels, value float64) error {
	return m.collector.SetGauge(name, m.withModule(labels), value)
}

func (m *moduleMeter) Observe(name string, labels Labels, value float64) error {
	return m.collector.Observe(name, m.withModule(labels), value)
}

func (m *moduleMeter) withModule(labels Labels) Labels {
	result := copyLabels(labels)
	result["module"] = m.module
	return result
}

func metricSeriesKey(descriptor MetricDescriptor, labels Labels) (string, Labels, error) {
	if len(labels) != len(descriptor.LabelNames) {
		return "", nil, fmt.Errorf("metric %s requires labels %v", descriptor.Name, descriptor.LabelNames)
	}
	safe := make(Labels, len(labels))
	parts := []string{descriptor.Name}
	for _, name := range descriptor.LabelNames {
		value, ok := labels[name]
		if !ok {
			return "", nil, fmt.Errorf("metric %s requires label %s", descriptor.Name, name)
		}
		value = strings.TrimSpace(value)
		if !validLabelValue(value) {
			return "", nil, fmt.Errorf("metric %s label %s has unsafe or high-cardinality value", descriptor.Name, name)
		}
		safe[name] = value
		parts = append(parts, name+"="+value)
	}
	return strings.Join(parts, "\x00"), safe, nil
}

func metricSnapshots(values map[string]*seriesState) []MetricSnapshot {
	result := make([]MetricSnapshot, 0, len(values))
	for _, state := range values {
		item := MetricSnapshot{Name: state.descriptor.Name, Type: state.descriptor.Type, Unit: state.descriptor.Unit, Labels: copyLabels(state.labels), Value: state.value, Count: state.count, Sum: state.sum}
		for index, count := range state.buckets {
			item.Buckets = append(item.Buckets, BucketSnapshot{UpperBound: state.descriptor.Buckets[index], Count: count})
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return labelsKey(result[i].Labels) < labelsKey(result[j].Labels)
	})
	return result
}

func databaseMetricSnapshots(value DatabaseSnapshot) []MetricSnapshot {
	connection := func(state string, amount int32) MetricSnapshot {
		return MetricSnapshot{Name: "campusos_db_pool_connections", Type: MetricGauge, Unit: "connections", Labels: Labels{"state": state}, Value: float64(amount)}
	}
	return []MetricSnapshot{
		connection("acquired", value.AcquiredConnections),
		connection("idle", value.IdleConnections),
		connection("total", value.TotalConnections),
		connection("maximum", value.MaximumConnections),
		{Name: "campusos_db_pool_acquire_total", Type: MetricCounter, Unit: "acquires", Value: float64(value.AcquireCount)},
		{Name: "campusos_db_pool_acquire_duration_seconds_total", Type: MetricCounter, Unit: "seconds", Value: value.AcquireDurationSeconds},
		{Name: "campusos_db_pool_acquire_errors_total", Type: MetricCounter, Unit: "errors", Labels: Labels{"kind": "canceled"}, Value: float64(value.CanceledAcquireCount)},
		{Name: "campusos_db_pool_acquire_errors_total", Type: MetricCounter, Unit: "errors", Labels: Labels{"kind": "empty_wait"}, Value: float64(value.EmptyAcquireCount)},
		{Name: "campusos_db_pool_empty_wait_seconds_total", Type: MetricCounter, Unit: "seconds", Value: value.EmptyAcquireWaitSeconds},
	}
}

func runtimeSnapshot() RuntimeSnapshot {
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return RuntimeSnapshot{
		Goroutines:     runtime.NumGoroutine(),
		HeapAllocBytes: stats.HeapAlloc,
		HeapObjects:    stats.HeapObjects,
	}
}

func runtimeMetricSnapshots(value RuntimeSnapshot) []MetricSnapshot {
	return []MetricSnapshot{
		{Name: "campusos_runtime_goroutines", Type: MetricGauge, Unit: "goroutines", Value: float64(value.Goroutines)},
		{Name: "campusos_runtime_heap_alloc_bytes", Type: MetricGauge, Unit: "bytes", Value: float64(value.HeapAllocBytes)},
	}
}

func writePrometheusSeries(out *strings.Builder, descriptor MetricDescriptor, item MetricSnapshot) {
	baseLabels := prometheusLabels(item.Labels, "")
	switch descriptor.Type {
	case MetricCounter, MetricGauge:
		fmt.Fprintf(out, "%s%s %s\n", descriptor.Name, baseLabels, formatFloat(item.Value))
	case MetricHistogram:
		for _, bucket := range item.Buckets {
			fmt.Fprintf(out, "%s_bucket%s %d\n", descriptor.Name, prometheusLabels(item.Labels, formatFloat(bucket.UpperBound)), bucket.Count)
		}
		fmt.Fprintf(out, "%s_bucket%s %d\n", descriptor.Name, prometheusLabels(item.Labels, "+Inf"), item.Count)
		fmt.Fprintf(out, "%s_sum%s %s\n", descriptor.Name, baseLabels, formatFloat(item.Sum))
		fmt.Fprintf(out, "%s_count%s %d\n", descriptor.Name, baseLabels, item.Count)
	}
}

func prometheusLabels(labels Labels, upperBound string) string {
	copy := copyLabels(labels)
	if upperBound != "" {
		copy["le"] = upperBound
	}
	if len(copy) == 0 {
		return ""
	}
	keys := make([]string, 0, len(copy))
	for key := range copy {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"").Replace(copy[key])
		parts = append(parts, key+"=\""+value+"\"")
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func normalizedOperation(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unmatched"
	}
	if !validLabelValue(value) {
		return "unknown"
	}
	return value
}

func labelsKey(labels Labels) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+labels[key])
	}
	return strings.Join(parts, ",")
}

func copyLabels(values Labels) Labels {
	result := make(Labels, len(values)+1)
	for key, value := range values {
		result[key] = value
	}
	return result
}

func copyInt64Map(values map[string]int64) map[string]int64 {
	clone := make(map[string]int64, len(values))
	for key, value := range values {
		clone[key] = value
	}
	return clone
}

func mustMetricCatalog(items []MetricDescriptor) map[string]MetricDescriptor {
	if err := ValidateMetricCatalog(items); err != nil {
		panic(err)
	}
	result := make(map[string]MetricDescriptor, len(items))
	for _, item := range items {
		result[item.Name] = item
	}
	return result
}

func validMetricName(value string) bool {
	if !strings.HasPrefix(value, "campusos_") {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '_' {
			continue
		}
		return false
	}
	return len(value) <= 128
}

func validLabelName(value string) bool {
	if value == "" || len(value) > 48 {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || char == '_' || (index > 0 && char >= '0' && char <= '9') {
			continue
		}
		return false
	}
	return true
}

func validLabelValue(value string) bool {
	if value == "" || len(value) > 128 || strings.ContainsAny(value, "@/\\?=&%\n\r\t ") {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || strings.ContainsRune("._:-", char) {
			continue
		}
		return false
	}
	return true
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'g', -1, 64)
}
