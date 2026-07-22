package observability

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

func TestMetricCatalogIsStableAndValid(t *testing.T) {
	items := MetricCatalog()
	if err := ValidateMetricCatalog(items); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"campusos_db_pool_acquire_duration_seconds_total",
		"campusos_db_pool_acquire_errors_total",
		"campusos_db_pool_acquire_total",
		"campusos_db_pool_connections",
		"campusos_db_pool_empty_wait_seconds_total",
		"campusos_email_delivery_duration_seconds",
		"campusos_email_delivery_total",
		"campusos_external_requests_total",
		"campusos_http_in_flight",
		"campusos_http_panics_total",
		"campusos_http_request_duration_seconds",
		"campusos_http_requests_total",
		"campusos_http_response_size_bytes",
		"campusos_identity_challenges_total",
		"campusos_identity_mfa_total",
		"campusos_identity_sessions_total",
		"campusos_module_operation_duration_seconds",
		"campusos_module_operations_total",
		"campusos_reliability_consumer_duration_seconds",
		"campusos_reliability_oldest_pending_age_seconds",
		"campusos_reliability_operations_total",
		"campusos_reliability_queue_events",
		"campusos_runtime_goroutines",
		"campusos_runtime_heap_alloc_bytes",
	}
	if len(items) != len(want) {
		t.Fatalf("catalog size=%d want=%d", len(items), len(want))
	}
	for index, name := range want {
		if items[index].Name != name {
			t.Fatalf("catalog[%d]=%s want=%s", index, items[index].Name, name)
		}
	}
}

func TestCollectorRejectsUnregisteredAndHighCardinalityLabels(t *testing.T) {
	collector := NewCollector()
	if err := collector.AddCounter("campusos_unknown_total", nil, 1); err == nil {
		t.Fatal("unregistered metric was accepted")
	}
	if err := collector.AddCounter("campusos_http_requests_total", Labels{
		"method": "GET", "operation": "http.test", "status_class": "2xx", "user_id": "42",
	}, 1); err == nil {
		t.Fatal("unregistered label was accepted")
	}
	if err := collector.AddCounter("campusos_http_requests_total", Labels{
		"method": "GET", "operation": "student@example.test", "status_class": "2xx",
	}, 1); err == nil {
		t.Fatal("email-like label value was accepted")
	}
	for index := 0; index < 128; index++ {
		if err := collector.AddCounter("campusos_external_requests_total", Labels{
			"integration": "provider-" + formatFloat(float64(index)), "result": "success",
		}, 1); err != nil {
			t.Fatalf("series %d: %v", index, err)
		}
	}
	if err := collector.AddCounter("campusos_external_requests_total", Labels{
		"integration": "provider-overflow", "result": "success",
	}, 1); err == nil {
		t.Fatal("series bound was not enforced")
	}
}

func TestModuleMeterIsConcurrentAndBounded(t *testing.T) {
	collector := NewCollector()
	meter := collector.ForModule("core.test")
	var wait sync.WaitGroup
	for index := 0; index < 100; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for count := 0; count < 20; count++ {
				if err := meter.AddCounter("campusos_module_operations_total", Labels{"operation": "read", "result": "success"}, 1); err != nil {
					t.Errorf("add metric: %v", err)
					return
				}
			}
		}()
	}
	wait.Wait()
	series := findMetric(t, collector.Snapshot(), "campusos_module_operations_total", Labels{"module": "core.test", "operation": "read", "result": "success"})
	if series.Value != 2000 {
		t.Fatalf("counter=%v want=2000", series.Value)
	}
}

func TestHTTPMiddlewareUsesStableOperationAndRecordsHistogram(t *testing.T) {
	gin.SetMode(gin.TestMode)
	collector := NewCollector()
	router := gin.New()
	router.Use(Middleware(collector))
	router.GET("/users/:id", func(c *gin.Context) {
		c.Set(RouteOperationContextKey, "http.identity.user.get")
		c.JSON(http.StatusCreated, gin.H{"ok": true})
	})
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/users/secret-user-id", nil))

	snapshot := collector.Snapshot()
	if snapshot.RequestTotal != 1 || snapshot.InFlight != 0 || snapshot.StatusCounts["201"] != 1 {
		t.Fatalf("compatibility snapshot=%+v", snapshot)
	}
	if _, exists := snapshot.RouteCounts["/users/secret-user-id"]; exists {
		t.Fatalf("raw request path became a metric label: %+v", snapshot.RouteCounts)
	}
	labels := Labels{"method": "GET", "operation": "http.identity.user.get", "status_class": "2xx"}
	if findMetric(t, snapshot, "campusos_http_requests_total", labels).Value != 1 {
		t.Fatal("request counter missing")
	}
	if findMetric(t, snapshot, "campusos_http_request_duration_seconds", labels).Count != 1 {
		t.Fatal("duration histogram missing")
	}
}

func TestPrometheusTextIncludesDatabaseStatsWithoutSensitiveValues(t *testing.T) {
	collector := NewCollector()
	collector.SetDatabaseStatsProvider(func() DatabaseSnapshot {
		return DatabaseSnapshot{AcquiredConnections: 2, IdleConnections: 3, TotalConnections: 5, MaximumConnections: 25, AcquireCount: 10, AcquireDurationSeconds: 0.25, CanceledAcquireCount: 1}
	})
	collector.RecordExternal("smtp.delivery", true)
	output := collector.PrometheusText()
	for _, expected := range []string{
		"# TYPE campusos_http_request_duration_seconds histogram",
		`campusos_db_pool_connections{state="acquired"} 2`,
		`campusos_external_requests_total{integration="smtp.delivery",result="success"} 1`,
		"# TYPE campusos_runtime_goroutines gauge",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("Prometheus output missing %q:\n%s", expected, output)
		}
	}
	for _, forbidden := range []string{"secret@example.test", "token=", "postgres://"} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("Prometheus output leaked %q", forbidden)
		}
	}
}

func TestSnapshotIncludesProcessCountersWithoutConfigurationData(t *testing.T) {
	snapshot := NewCollector().Snapshot()
	if snapshot.Runtime.Goroutines < 1 {
		t.Fatalf("goroutines=%d want positive runtime count", snapshot.Runtime.Goroutines)
	}
	if snapshot.Runtime.HeapAllocBytes == 0 {
		t.Fatal("heap allocation counter was empty")
	}
}

func TestPrometheusRuleExamplesAreValidYAMLAndUseRegisteredMetrics(t *testing.T) {
	content, err := os.ReadFile("../../deploy/prometheus/campusos-v13.rules.yml")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := yaml.Unmarshal(content, &document); err != nil {
		t.Fatalf("parse Prometheus rules: %v", err)
	}
	text := string(content)
	for _, expected := range []string{
		"CampusOSReliabilityWorkerStopped",
		"CampusOSEmailDeliveryDegraded",
		"CampusOSRefreshTokenReuseDetected",
		"campusos_reliability_operations_total",
		"campusos_reliability_oldest_pending_age_seconds",
		"campusos_email_delivery_total",
		"campusos_identity_sessions_total",
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("Prometheus rules do not reference %q", expected)
		}
	}
}

func findMetric(t *testing.T, snapshot Snapshot, name string, labels Labels) MetricSnapshot {
	t.Helper()
	key := labelsKey(labels)
	for _, item := range snapshot.Metrics {
		if item.Name == name && labelsKey(item.Labels) == key {
			return item
		}
	}
	t.Fatalf("metric %s labels=%v was not found", name, labels)
	return MetricSnapshot{}
}
