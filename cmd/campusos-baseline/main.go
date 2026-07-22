package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	platformversion "github.com/campusos/CampusOS/internal/platform/version"
	"gopkg.in/yaml.v3"
)

const baselineSchema = "campusos.v13-baseline/v1"

type snapshot struct {
	Schema             string              `json:"schema"`
	GeneratedAt        time.Time           `json:"generated_at"`
	ApplicationVersion string              `json:"application_version"`
	Git                gitSnapshot         `json:"git"`
	Environment        environmentSnapshot `json:"environment"`
	Contracts          contractSnapshot    `json:"contracts"`
	Database           databaseSnapshot    `json:"database"`
	Modules            moduleSnapshot      `json:"modules"`
	ExternalPlugins    pluginSnapshot      `json:"external_plugins"`
	Resources          resourceSnapshot    `json:"resources"`
	StructuredQueries  []querySnapshot     `json:"structured_queries"`
	HTTP               []httpSnapshot      `json:"http,omitempty"`
}

type gitSnapshot struct {
	Commit string `json:"commit"`
	Dirty  bool   `json:"dirty"`
}

type environmentSnapshot struct {
	GOOS        string `json:"goos"`
	GOARCH      string `json:"goarch"`
	GoVersion   string `json:"go_version"`
	LogicalCPUs int    `json:"logical_cpus"`
	MemoryKiB   int64  `json:"memory_kib,omitempty"`
}

type contractSnapshot struct {
	Version        string         `json:"version"`
	Routes         int            `json:"routes"`
	AudienceCounts map[string]int `json:"audience_counts"`
	SHA256         string         `json:"sha256"`
}

type databaseSnapshot struct {
	MigrationCount  int    `json:"migration_count"`
	LatestMigration string `json:"latest_migration"`
	SHA256          string `json:"sha256"`
}

type moduleSnapshot struct {
	Count      int            `json:"count"`
	KindCounts map[string]int `json:"kind_counts"`
	IDs        []string       `json:"ids"`
	SHA256     string         `json:"sha256"`
}

type pluginSnapshot struct {
	Count    int            `json:"count"`
	Runtimes map[string]int `json:"runtime_counts"`
	Names    []string       `json:"names"`
	SHA256   string         `json:"sha256"`
}

type resourceSnapshot struct {
	Count      int            `json:"count"`
	TypeCounts map[string]int `json:"type_counts"`
	Packages   []resourceItem `json:"packages"`
	SHA256     string         `json:"sha256"`
}

type resourceItem struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Version  string `json:"version"`
	TreeHash string `json:"tree_sha256"`
}

type querySnapshot struct {
	Feature              string `json:"feature"`
	DetailFetchMode      string `json:"detail_fetch_mode"`
	DetailReadsForNItems string `json:"detail_reads_for_n_items"`
	BudgetState          string `json:"budget_state"`
}

type httpSnapshot struct {
	Path          string  `json:"path"`
	Samples       int     `json:"samples"`
	Status        int     `json:"status"`
	ResponseBytes int64   `json:"response_bytes"`
	Total         *int64  `json:"total,omitempty"`
	P50MS         float64 `json:"p50_ms"`
	P95MS         float64 `json:"p95_ms"`
	P99MS         float64 `json:"p99_ms"`
}

type routeFile struct {
	Version string `json:"version"`
	Routes  []struct {
		Audience string `json:"audience"`
	} `json:"routes"`
}

type moduleManifest struct {
	ID   string `yaml:"id"`
	Kind string `yaml:"kind"`
}

type pluginManifest struct {
	Name    string `yaml:"name"`
	Runtime string `yaml:"runtime"`
}

type resourceManifest struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Version string `json:"version"`
}

func main() {
	root := flag.String("root", ".", "CampusOS repository root")
	output := flag.String("output", "", "write JSON to this path instead of stdout")
	live := flag.Bool("live", false, "sample loopback HTTP endpoints")
	baseURL := flag.String("base-url", "http://localhost:8080", "CampusOS API origin for live sampling")
	samples := flag.Int("samples", 15, "samples per HTTP endpoint")
	flag.Parse()

	result, err := collect(*root, *live, *baseURL, *samples)
	if err != nil {
		fmt.Fprintln(os.Stderr, "baseline collection failed:", err)
		os.Exit(1)
	}
	payload, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "encode baseline:", err)
		os.Exit(1)
	}
	payload = append(payload, '\n')
	if strings.TrimSpace(*output) == "" {
		_, _ = os.Stdout.Write(payload)
		return
	}
	if err := os.WriteFile(*output, payload, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write baseline:", err)
		os.Exit(1)
	}
}

func collect(root string, live bool, baseURL string, samples int) (*snapshot, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	result := &snapshot{
		Schema: baselineSchema, GeneratedAt: time.Now().UTC(), ApplicationVersion: platformversion.Number,
		Environment: environmentSnapshot{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, GoVersion: runtime.Version(), LogicalCPUs: runtime.NumCPU(), MemoryKiB: memoryKiB()},
	}
	if result.Git, err = collectGit(absRoot); err != nil {
		return nil, err
	}
	if result.Contracts, err = collectContracts(absRoot); err != nil {
		return nil, err
	}
	if result.Database, err = collectMigrations(absRoot); err != nil {
		return nil, err
	}
	if result.Modules, err = collectModules(absRoot); err != nil {
		return nil, err
	}
	if result.ExternalPlugins, err = collectPlugins(absRoot); err != nil {
		return nil, err
	}
	if result.Resources, err = collectResources(absRoot); err != nil {
		return nil, err
	}
	result.StructuredQueries = collectStructuredQueries(absRoot)
	if live {
		if samples < 3 || samples > 100 {
			return nil, errors.New("samples must be between 3 and 100")
		}
		if err := requireLoopback(baseURL); err != nil {
			return nil, err
		}
		for _, path := range []string{
			"/api/v1/health",
			"/api/v1/threads?page=1&page_size=20",
			"/api/v1/mutual-aid/threads?page=1&page_size=20",
			"/api/v1/secondhand/threads?page=1&page_size=20",
		} {
			measurement, measureErr := measureHTTP(baseURL, path, samples)
			if measureErr != nil {
				return nil, measureErr
			}
			result.HTTP = append(result.HTTP, measurement)
		}
	}
	return result, nil
}

func collectGit(root string) (gitSnapshot, error) {
	commit, err := commandOutput(root, "git", "rev-parse", "HEAD")
	if err != nil {
		return gitSnapshot{}, err
	}
	status, err := commandOutput(root, "git", "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return gitSnapshot{}, err
	}
	return gitSnapshot{Commit: commit, Dirty: status != ""}, nil
}

func collectContracts(root string) (contractSnapshot, error) {
	path := filepath.Join(root, "docs/api/http-routes-v0.6.json")
	payload, err := os.ReadFile(path)
	if err != nil {
		return contractSnapshot{}, err
	}
	var contract routeFile
	if err := json.Unmarshal(payload, &contract); err != nil {
		return contractSnapshot{}, err
	}
	counts := map[string]int{}
	for _, route := range contract.Routes {
		counts[route.Audience]++
	}
	return contractSnapshot{Version: contract.Version, Routes: len(contract.Routes), AudienceCounts: counts, SHA256: digestBytes(payload)}, nil
}

func collectMigrations(root string) (databaseSnapshot, error) {
	paths, err := filepath.Glob(filepath.Join(root, "migrations/*.up.sql"))
	if err != nil || len(paths) == 0 {
		return databaseSnapshot{}, errors.New("no migration files found")
	}
	sort.Strings(paths)
	return databaseSnapshot{MigrationCount: len(paths), LatestMigration: strings.TrimSuffix(filepath.Base(paths[len(paths)-1]), ".up.sql"), SHA256: digestFiles(root, paths)}, nil
}

func collectModules(root string) (moduleSnapshot, error) {
	paths, err := globRecursive(filepath.Join(root, "modules"), "module.yaml")
	if err != nil {
		return moduleSnapshot{}, err
	}
	result := moduleSnapshot{Count: len(paths), KindCounts: map[string]int{}, SHA256: digestFiles(root, paths)}
	for _, path := range paths {
		var manifest moduleManifest
		if err := decodeYAML(path, &manifest); err != nil {
			return moduleSnapshot{}, err
		}
		result.KindCounts[manifest.Kind]++
		result.IDs = append(result.IDs, manifest.ID)
	}
	sort.Strings(result.IDs)
	return result, nil
}

func collectPlugins(root string) (pluginSnapshot, error) {
	paths, err := globRecursive(filepath.Join(root, "data/plugins"), "plugin.yaml")
	if err != nil {
		return pluginSnapshot{}, err
	}
	result := pluginSnapshot{Count: len(paths), Runtimes: map[string]int{}, SHA256: digestFiles(root, paths)}
	for _, path := range paths {
		var manifest pluginManifest
		if err := decodeYAML(path, &manifest); err != nil {
			return pluginSnapshot{}, err
		}
		result.Runtimes[manifest.Runtime]++
		result.Names = append(result.Names, manifest.Name)
	}
	sort.Strings(result.Names)
	return result, nil
}

func collectResources(root string) (resourceSnapshot, error) {
	paths, err := globRecursive(filepath.Join(root, "data/resources"), "resource.json")
	if err != nil {
		return resourceSnapshot{}, err
	}
	result := resourceSnapshot{Count: len(paths), TypeCounts: map[string]int{}, SHA256: digestFiles(root, paths)}
	for _, path := range paths {
		payload, readErr := os.ReadFile(path)
		if readErr != nil {
			return resourceSnapshot{}, readErr
		}
		var manifest resourceManifest
		if err := json.Unmarshal(payload, &manifest); err != nil {
			return resourceSnapshot{}, err
		}
		packageFiles, err := globAllFiles(filepath.Dir(path))
		if err != nil {
			return resourceSnapshot{}, err
		}
		result.TypeCounts[manifest.Type]++
		result.Packages = append(result.Packages, resourceItem{ID: manifest.ID, Type: manifest.Type, Version: manifest.Version, TreeHash: digestFiles(root, packageFiles)})
	}
	sort.Slice(result.Packages, func(i, j int) bool { return result.Packages[i].ID < result.Packages[j].ID })
	return result, nil
}

func collectStructuredQueries(root string) []querySnapshot {
	result := make([]querySnapshot, 0, 2)
	for _, feature := range []string{"mutualaid", "secondhand"} {
		path := filepath.Join(root, "internal/modules/features", feature, "service.go")
		payload, _ := os.ReadFile(path)
		mode, reads, state := "unknown", "unknown", "unknown"
		if strings.Contains(string(payload), "s.store.Get(ctx, thread.ID)") {
			mode, reads, state = "per-item Store.Get", "N", "violates constant-query target"
		} else if strings.Contains(string(payload), "s.store.GetMany(") {
			mode, reads, state = "batch Store.GetMany", "1", "within constant-query target"
		}
		result = append(result, querySnapshot{Feature: feature, DetailFetchMode: mode, DetailReadsForNItems: reads, BudgetState: state})
	}
	return result
}

func measureHTTP(baseURL, path string, samples int) (httpSnapshot, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	measurements := make([]float64, 0, samples)
	result := httpSnapshot{Path: path, Samples: samples}
	for attempt := -1; attempt < samples; attempt++ {
		started := time.Now()
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, strings.TrimRight(baseURL, "/")+path, nil)
		if err != nil {
			return httpSnapshot{}, err
		}
		response, err := client.Do(request)
		if err != nil {
			return httpSnapshot{}, fmt.Errorf("GET %s: %w", path, err)
		}
		payload, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
		_ = response.Body.Close()
		if readErr != nil {
			return httpSnapshot{}, readErr
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return httpSnapshot{}, fmt.Errorf("GET %s returned %d", path, response.StatusCode)
		}
		if attempt < 0 {
			continue
		}
		result.Status = response.StatusCode
		result.ResponseBytes = int64(len(payload))
		measurements = append(measurements, float64(time.Since(started).Microseconds())/1000)
		if result.Total == nil {
			result.Total = responseTotal(payload)
		}
	}
	sort.Float64s(measurements)
	result.P50MS = percentile(measurements, 0.50)
	result.P95MS = percentile(measurements, 0.95)
	result.P99MS = percentile(measurements, 0.99)
	return result, nil
}

func responseTotal(payload []byte) *int64 {
	var envelope struct {
		Data struct {
			Total *int64 `json:"total"`
		} `json:"data"`
	}
	if json.Unmarshal(payload, &envelope) != nil {
		return nil
	}
	return envelope.Data.Total
}

func percentile(values []float64, ratio float64) float64 {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * ratio)
	return values[index]
}

func requireLoopback(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	host := parsed.Hostname()
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("live baseline target must be loopback")
	}
	return nil
}

func decodeYAML(path string, target any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(payload, target); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func globRecursive(root, name string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == name {
			paths = append(paths, path)
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func globAllFiles(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	sort.Strings(paths)
	return paths, err
}

func digestFiles(root string, paths []string) string {
	hash := sha256.New()
	for _, path := range paths {
		relative, _ := filepath.Rel(root, path)
		_, _ = io.WriteString(hash, filepath.ToSlash(relative)+"\x00")
		payload, err := os.ReadFile(path)
		if err == nil {
			_, _ = hash.Write(payload)
		}
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func digestBytes(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func commandOutput(dir, name string, args ...string) (string, error) {
	command := exec.Command(name, args...)
	command.Dir = dir
	payload, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	return strings.TrimSpace(string(payload)), nil
}

func memoryKiB() int64 {
	payload, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	var value int64
	_, _ = fmt.Sscanf(string(payload), "MemTotal: %d kB", &value)
	return value
}
