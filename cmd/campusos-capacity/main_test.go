package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPercentileUsesNearestRank(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5}
	if got := percentile(values, 0.50); got != 3 {
		t.Fatalf("p50=%v want=3", got)
	}
	if got := percentile(values, 0.95); got != 5 {
		t.Fatalf("p95=%v want=5", got)
	}
}

func TestRequireLoopbackRejectsRemoteAndCredentials(t *testing.T) {
	for _, target := range []string{"http://localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		if err := requireLoopback(target); err != nil {
			t.Fatalf("target %q: %v", target, err)
		}
	}
	for _, target := range []string{"https://example.test", "ftp://localhost", "http://token@localhost:8080"} {
		if err := requireLoopback(target); err == nil {
			t.Fatalf("unsafe target %q was accepted", target)
		}
	}
}

func TestCompareReportsDetectsLatencyAndProtectedResourceRegression(t *testing.T) {
	baseline := report{
		Schema:      reportSchema,
		Endpoints:   []endpointResult{{Name: "health", StatusCounts: map[string]int{"200": 3}, P95MS: 10}},
		Runtime:     &runtimeSnapshot{Goroutines: 10, HeapAllocBytes: 100, Database: &databaseSnapshot{}},
		Reliability: &reliabilityStats{Health: "healthy"},
	}
	candidate := report{
		Schema:      reportSchema,
		Endpoints:   []endpointResult{{Name: "health", StatusCounts: map[string]int{"200": 3}, P95MS: 13}},
		Runtime:     &runtimeSnapshot{Goroutines: 13, HeapAllocBytes: 160, Database: &databaseSnapshot{EmptyAcquireWaitSeconds: 2}},
		Reliability: &reliabilityStats{Health: "degraded", Dead: 1, OldestPendingAgeSeconds: 400},
	}
	limits := budget{MaxP95RegressionRatio: 0.2, MaxGoroutineGrowthRatio: 0.2, MaxHeapAllocGrowthRatio: 0.5, MaxDBEmptyWaitSeconds: 1, MaxQueueAgeSeconds: 300, RequireSuccessfulRequests: true}
	if got := compareReports(baseline, candidate, limits); len(got) < 5 {
		t.Fatalf("violations=%v want latency/resource/reliability failures", got)
	}
}

func TestCompareReportsOnlyUsesConfiguredAbsoluteAllowanceForLowLatencyNoise(t *testing.T) {
	baseline := report{Schema: reportSchema, Endpoints: []endpointResult{{Name: "health", P95MS: 2}}}
	limits := budget{MaxP95RegressionRatio: 0.2, MaxP95AbsoluteRegressionMS: 2}
	lowNoise := report{Schema: reportSchema, Endpoints: []endpointResult{{Name: "health", P95MS: 3.5}}}
	if got := compareReports(baseline, lowNoise, limits); len(got) != 0 {
		t.Fatalf("low-latency scheduling noise should stay within the explicit allowance: %v", got)
	}
	realRegression := report{Schema: reportSchema, Endpoints: []endpointResult{{Name: "health", P95MS: 4.1}}}
	if got := compareReports(baseline, realRegression, limits); len(got) != 1 {
		t.Fatalf("larger regression must remain visible: %v", got)
	}
}

func TestReadBudgetRejectsNegativeAbsoluteLatencyAllowance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "budget.json")
	if err := os.WriteFile(path, []byte(`{"schema":"campusos.capacity-budget/v1","max_p95_regression_ratio":0.2,"max_p95_absolute_regression_ms":-1,"max_goroutine_growth_ratio":0.2,"max_heap_alloc_growth_ratio":0.5,"max_db_empty_wait_seconds":1,"max_queue_age_seconds":300}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readBudget(path); err == nil {
		t.Fatal("negative absolute latency allowance was accepted")
	}
}

func TestAuthorizationFileRequiresPrivateMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "authorization")
	if err := os.WriteFile(path, []byte("Bearer safe-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := readAuthorizationFile(path)
	if err != nil || value != "Bearer safe-token" {
		t.Fatalf("read secure file value=%q err=%v", value, err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readAuthorizationFile(path); err == nil {
		t.Fatal("world-readable authorization file was accepted")
	}
}

func TestProtectedEndpointCatalogUsesFixedBoundedReadPaths(t *testing.T) {
	want := map[string]bool{
		"admin_admission_list": false,
		"admin_mfa_policy":     false,
		"metrics_summary":      false,
		"reliability_summary":  false,
	}
	for _, endpoint := range protectedEndpoints {
		if !strings.HasPrefix(endpoint.Path, "/api/v1/") {
			t.Fatalf("protected endpoint %q has unsafe path %q", endpoint.Name, endpoint.Path)
		}
		if _, ok := want[endpoint.Name]; ok {
			want[endpoint.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("protected endpoint catalog is missing %q", name)
		}
	}
}
