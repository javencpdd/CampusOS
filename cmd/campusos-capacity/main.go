// campusos-capacity captures a small, repeatable, loopback-only capacity
// sample and compares two samples against a conservative release budget.
// It intentionally never accepts an access token on the command line.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"
)

const reportSchema = "campusos.capacity/v1"

type report struct {
	Schema      string            `json:"schema"`
	GeneratedAt time.Time         `json:"generated_at"`
	Target      string            `json:"target"`
	Samples     int               `json:"samples"`
	Endpoints   []endpointResult  `json:"endpoints"`
	Runtime     *runtimeSnapshot  `json:"runtime,omitempty"`
	Reliability *reliabilityStats `json:"reliability,omitempty"`
}

type endpointDefinition struct {
	Name string
	Path string
}

type endpointResult struct {
	Name         string         `json:"name"`
	Path         string         `json:"path"`
	Samples      int            `json:"samples"`
	StatusCounts map[string]int `json:"status_counts"`
	ErrorCount   int            `json:"error_count"`
	P50MS        float64        `json:"p50_ms"`
	P95MS        float64        `json:"p95_ms"`
	P99MS        float64        `json:"p99_ms"`
}

type runtimeSnapshot struct {
	Goroutines     int               `json:"goroutines"`
	HeapAllocBytes uint64            `json:"heap_alloc_bytes"`
	Database       *databaseSnapshot `json:"database,omitempty"`
}

type databaseSnapshot struct {
	EmptyAcquireWaitSeconds float64 `json:"empty_acquire_wait_seconds"`
}

type reliabilityStats struct {
	Health                  string  `json:"health"`
	Dead                    int64   `json:"dead"`
	OldestPendingAgeSeconds float64 `json:"oldest_pending_age_seconds"`
}

// budget is deliberately small and environment independent. Operators should
// version a file next to their candidate evidence instead of tuning limits in
// CI arguments where they are hard to review.
type budget struct {
	Schema                     string  `json:"schema"`
	MaxP95RegressionRatio      float64 `json:"max_p95_regression_ratio"`
	MaxP95AbsoluteRegressionMS float64 `json:"max_p95_absolute_regression_ms"`
	MaxGoroutineGrowthRatio    float64 `json:"max_goroutine_growth_ratio"`
	MaxHeapAllocGrowthRatio    float64 `json:"max_heap_alloc_growth_ratio"`
	MaxDBEmptyWaitSeconds      float64 `json:"max_db_empty_wait_seconds"`
	MaxQueueAgeSeconds         float64 `json:"max_queue_age_seconds"`
	RequireSuccessfulRequests  bool    `json:"require_successful_requests"`
}

var publicEndpoints = []endpointDefinition{
	{Name: "health", Path: "/api/v1/health"},
	{Name: "thread_list", Path: "/api/v1/threads?page=1&page_size=20"},
	{Name: "mutual_aid_list", Path: "/api/v1/mutual-aid/threads?page=1&page_size=20"},
	{Name: "secondhand_list", Path: "/api/v1/secondhand/threads?page=1&page_size=20"},
}

// protectedEndpoints are intentionally fixed, bounded read paths. They make
// the privileged part of the capacity report comparable without accepting a
// user ID, free-form query, or credential through a command-line argument.
var protectedEndpoints = []endpointDefinition{
	{Name: "admin_admission_list", Path: "/api/v1/identity/admin-accounts?page=1&page_size=20"},
	{Name: "admin_mfa_policy", Path: "/api/v1/identity/mfa-policy"},
	{Name: "metrics_summary", Path: "/api/v1/metrics/summary"},
	{Name: "reliability_summary", Path: "/api/v1/platform/reliability/summary"},
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "capture":
		err = captureCommand(os.Args[2:])
	case "compare":
		err = compareCommand(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "campusos-capacity:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Usage:
  campusos-capacity capture [flags]
  campusos-capacity compare [flags]

capture only targets a loopback CampusOS API. It never accepts credentials on
the command line. Use a mode-0600 authorization file only when protected
observability snapshots are needed.
`)
}

func captureCommand(args []string) error {
	flags := flag.NewFlagSet("capture", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	baseURL := flags.String("base-url", "http://localhost:8080", "loopback CampusOS API origin")
	samples := flags.Int("samples", 15, "successful measurement attempts per public endpoint (3-100)")
	output := flags.String("output", "", "write JSON report to this path instead of stdout")
	includeAdmin := flags.Bool("include-admin-observability", false, "capture protected runtime and reliability snapshots")
	authorizationFile := flags.String("authorization-file", "", "mode-0600 local file containing one Authorization header value")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *samples < 3 || *samples > 100 {
		return errors.New("samples must be between 3 and 100")
	}
	if err := requireLoopback(*baseURL); err != nil {
		return err
	}
	if *includeAdmin && strings.TrimSpace(*authorizationFile) == "" {
		return errors.New("--include-admin-observability requires --authorization-file")
	}
	authorization := ""
	var err error
	if strings.TrimSpace(*authorizationFile) != "" {
		authorization, err = readAuthorizationFile(*authorizationFile)
		if err != nil {
			return err
		}
	}

	client := &http.Client{Timeout: 8 * time.Second}
	result := report{Schema: reportSchema, GeneratedAt: time.Now().UTC(), Target: strings.TrimRight(*baseURL, "/"), Samples: *samples}
	for _, endpoint := range publicEndpoints {
		measurement, measureErr := measureEndpoint(context.Background(), client, *baseURL, endpoint, *samples, "")
		if measureErr != nil {
			return measureErr
		}
		result.Endpoints = append(result.Endpoints, measurement)
	}
	if *includeAdmin {
		for _, endpoint := range protectedEndpoints {
			measurement, measureErr := measureEndpoint(context.Background(), client, *baseURL, endpoint, *samples, authorization)
			if measureErr != nil {
				return measureErr
			}
			result.Endpoints = append(result.Endpoints, measurement)
		}
		runtime, runtimeErr := captureRuntime(context.Background(), client, *baseURL, authorization)
		if runtimeErr != nil {
			return runtimeErr
		}
		reliability, reliabilityErr := captureReliability(context.Background(), client, *baseURL, authorization)
		if reliabilityErr != nil {
			return reliabilityErr
		}
		result.Runtime = runtime
		result.Reliability = reliability
	}
	return writeJSON(*output, result)
}

func compareCommand(args []string) error {
	flags := flag.NewFlagSet("compare", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	baselinePath := flags.String("baseline", "", "previous JSON capacity report")
	candidatePath := flags.String("candidate", "", "candidate JSON capacity report")
	budgetPath := flags.String("budget", "", "JSON capacity budget")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*baselinePath) == "" || strings.TrimSpace(*candidatePath) == "" || strings.TrimSpace(*budgetPath) == "" {
		return errors.New("--baseline, --candidate, and --budget are required")
	}
	baseline, err := readReport(*baselinePath)
	if err != nil {
		return fmt.Errorf("read baseline: %w", err)
	}
	candidate, err := readReport(*candidatePath)
	if err != nil {
		return fmt.Errorf("read candidate: %w", err)
	}
	limits, err := readBudget(*budgetPath)
	if err != nil {
		return fmt.Errorf("read budget: %w", err)
	}
	violations := compareReports(baseline, candidate, limits)
	if len(violations) > 0 {
		return errors.New(strings.Join(violations, "; "))
	}
	fmt.Println("capacity comparison passed")
	return nil
}

func measureEndpoint(ctx context.Context, client *http.Client, baseURL string, endpoint endpointDefinition, samples int, authorization string) (endpointResult, error) {
	result := endpointResult{Name: endpoint.Name, Path: endpoint.Path, Samples: samples, StatusCounts: map[string]int{}}
	latencies := make([]float64, 0, samples)
	for attempt := -1; attempt < samples; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+endpoint.Path, nil)
		if err != nil {
			return endpointResult{}, err
		}
		if authorization != "" {
			request.Header.Set("Authorization", authorization)
		}
		started := time.Now()
		response, err := client.Do(request)
		elapsed := float64(time.Since(started).Microseconds()) / 1000
		if err != nil {
			if attempt >= 0 {
				result.ErrorCount++
			}
			continue
		}
		_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 4<<20))
		_ = response.Body.Close()
		if attempt >= 0 {
			result.StatusCounts[fmt.Sprintf("%d", response.StatusCode)]++
			if readErr != nil {
				result.ErrorCount++
				continue
			}
			if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
				result.ErrorCount++
				continue
			}
			latencies = append(latencies, elapsed)
		}
	}
	if len(latencies) == 0 {
		return result, nil
	}
	sort.Float64s(latencies)
	result.P50MS = percentile(latencies, 0.50)
	result.P95MS = percentile(latencies, 0.95)
	result.P99MS = percentile(latencies, 0.99)
	return result, nil
}

func captureRuntime(ctx context.Context, client *http.Client, baseURL, authorization string) (*runtimeSnapshot, error) {
	payload, err := getEnvelopeData(ctx, client, baseURL, "/api/v1/metrics/summary", authorization)
	if err != nil {
		return nil, err
	}
	var value struct {
		Runtime struct {
			Goroutines     int    `json:"goroutines"`
			HeapAllocBytes uint64 `json:"heap_alloc_bytes"`
		} `json:"runtime"`
		Database *databaseSnapshot `json:"database"`
	}
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, fmt.Errorf("decode metrics snapshot: %w", err)
	}
	return &runtimeSnapshot{Goroutines: value.Runtime.Goroutines, HeapAllocBytes: value.Runtime.HeapAllocBytes, Database: value.Database}, nil
}

func captureReliability(ctx context.Context, client *http.Client, baseURL, authorization string) (*reliabilityStats, error) {
	payload, err := getEnvelopeData(ctx, client, baseURL, "/api/v1/platform/reliability/summary", authorization)
	if err != nil {
		return nil, err
	}
	var value reliabilityStats
	if err := json.Unmarshal(payload, &value); err != nil {
		return nil, fmt.Errorf("decode reliability summary: %w", err)
	}
	return &value, nil
}

func getEnvelopeData(ctx context.Context, client *http.Client, baseURL, path, authorization string) (json.RawMessage, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", authorization)
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	payload, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	_ = response.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("GET %s returned %d", path, response.StatusCode)
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil, fmt.Errorf("GET %s returned no data", path)
	}
	return envelope.Data, nil
}

func readAuthorizationFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return "", errors.New("authorization file must be a regular mode-0600 file")
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(payload))
	if value == "" || strings.ContainsAny(value, "\r\n") || len(value) > 8192 {
		return "", errors.New("authorization file must contain one bounded header value")
	}
	return value, nil
}

func readReport(path string) (report, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return report{}, err
	}
	var value report
	if err := json.Unmarshal(payload, &value); err != nil {
		return report{}, err
	}
	if value.Schema != reportSchema || len(value.Endpoints) == 0 {
		return report{}, errors.New("unsupported or incomplete capacity report")
	}
	return value, nil
}

func readBudget(path string) (budget, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return budget{}, err
	}
	var value budget
	if err := json.Unmarshal(payload, &value); err != nil {
		return budget{}, err
	}
	if value.Schema != "campusos.capacity-budget/v1" {
		return budget{}, errors.New("unsupported capacity budget schema")
	}
	for _, limit := range []float64{value.MaxP95RegressionRatio, value.MaxP95AbsoluteRegressionMS, value.MaxGoroutineGrowthRatio, value.MaxHeapAllocGrowthRatio, value.MaxDBEmptyWaitSeconds, value.MaxQueueAgeSeconds} {
		if math.IsNaN(limit) || math.IsInf(limit, 0) || limit < 0 {
			return budget{}, errors.New("capacity budget contains an invalid limit")
		}
	}
	return value, nil
}

func compareReports(baseline, candidate report, limits budget) []string {
	violations := make([]string, 0)
	baselineEndpoints := make(map[string]endpointResult, len(baseline.Endpoints))
	for _, endpoint := range baseline.Endpoints {
		baselineEndpoints[endpoint.Name] = endpoint
	}
	for _, current := range candidate.Endpoints {
		previous, exists := baselineEndpoints[current.Name]
		if !exists {
			violations = append(violations, "candidate has an unbaselined endpoint "+current.Name)
			continue
		}
		if limits.RequireSuccessfulRequests && (previous.ErrorCount > 0 || current.ErrorCount > 0 || !allSuccessful(previous.StatusCounts) || !allSuccessful(current.StatusCounts)) {
			violations = append(violations, current.Name+" has failed or non-2xx requests")
			continue
		}
		if previous.P95MS > 0 && current.P95MS > previous.P95MS*(1+limits.MaxP95RegressionRatio) && current.P95MS-previous.P95MS > limits.MaxP95AbsoluteRegressionMS {
			violations = append(violations, fmt.Sprintf("%s p95 %.2fms exceeds baseline %.2fms plus %.0f%% and %.2fms absolute allowance", current.Name, current.P95MS, previous.P95MS, limits.MaxP95RegressionRatio*100, limits.MaxP95AbsoluteRegressionMS))
		}
	}
	for name := range baselineEndpoints {
		found := false
		for _, current := range candidate.Endpoints {
			if current.Name == name {
				found = true
				break
			}
		}
		if !found {
			violations = append(violations, "candidate omitted baseline endpoint "+name)
		}
	}
	violations = append(violations, compareRuntime(baseline.Runtime, candidate.Runtime, limits)...)
	violations = append(violations, compareReliability(baseline.Reliability, candidate.Reliability, limits)...)
	return violations
}

func compareRuntime(baseline, candidate *runtimeSnapshot, limits budget) []string {
	if baseline == nil {
		return nil
	}
	if candidate == nil {
		return []string{"candidate omitted protected runtime snapshot"}
	}
	violations := make([]string, 0)
	if exceedsGrowth(float64(baseline.Goroutines), float64(candidate.Goroutines), limits.MaxGoroutineGrowthRatio) {
		violations = append(violations, "goroutine growth exceeds budget")
	}
	if exceedsGrowth(float64(baseline.HeapAllocBytes), float64(candidate.HeapAllocBytes), limits.MaxHeapAllocGrowthRatio) {
		violations = append(violations, "heap allocation growth exceeds budget")
	}
	if baseline.Database != nil {
		if candidate.Database == nil {
			violations = append(violations, "candidate omitted database runtime snapshot")
		} else if candidate.Database.EmptyAcquireWaitSeconds > limits.MaxDBEmptyWaitSeconds {
			violations = append(violations, "database empty-pool wait exceeds budget")
		}
	}
	return violations
}

func compareReliability(baseline, candidate *reliabilityStats, limits budget) []string {
	if baseline == nil {
		return nil
	}
	if candidate == nil {
		return []string{"candidate omitted protected reliability snapshot"}
	}
	violations := make([]string, 0)
	if candidate.Dead > 0 {
		violations = append(violations, "reliability dead-letter queue is non-empty")
	}
	if candidate.OldestPendingAgeSeconds > limits.MaxQueueAgeSeconds {
		violations = append(violations, "reliability pending age exceeds budget")
	}
	if candidate.Health != "" && candidate.Health != "healthy" {
		violations = append(violations, "reliability health is "+candidate.Health)
	}
	return violations
}

func exceedsGrowth(baseline, candidate, allowedRatio float64) bool {
	if baseline <= 0 {
		return false
	}
	return candidate > baseline*(1+allowedRatio)
}

func allSuccessful(statuses map[string]int) bool {
	if len(statuses) == 0 {
		return false
	}
	for status, count := range statuses {
		if count <= 0 || len(status) != 3 || status[0] != '2' {
			return false
		}
	}
	return true
}

func percentile(values []float64, ratio float64) float64 {
	if len(values) == 0 {
		return 0
	}
	index := int(math.Ceil(float64(len(values))*ratio)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func requireLoopback(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("capacity target must use http or https")
	}
	if parsed.User != nil || parsed.Hostname() == "" {
		return errors.New("capacity target must have a host and no URL credentials")
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("capacity target must be loopback")
	}
	return nil
}

func writeJSON(path string, value any) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if strings.TrimSpace(path) == "" {
		_, err = os.Stdout.Write(payload)
		return err
	}
	return os.WriteFile(path, payload, 0o644)
}
