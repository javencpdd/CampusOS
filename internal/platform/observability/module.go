package observability

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	metrics "github.com/campusos/CampusOS/pkg/observability"
)

const (
	ModuleID      = "core.observability"
	PortMeter     = "platform.observability.meter"
	PortCollector = "platform.observability.collector"
)

type Config struct {
	PrometheusEnabled bool
	PrometheusAddr    string
	PrometheusPath    string
}

type Module struct {
	collector *metrics.Collector
	config    Config
	listen    func(string, string) (net.Listener, error)

	mu       sync.RWMutex
	server   *http.Server
	listener net.Listener
	serveErr error
}

func NewModule(collector *metrics.Collector, config Config) *Module {
	if collector == nil {
		collector = metrics.NewCollector()
	}
	if strings.TrimSpace(config.PrometheusAddr) == "" {
		config.PrometheusAddr = "127.0.0.1:9091"
	}
	if strings.TrimSpace(config.PrometheusPath) == "" {
		config.PrometheusPath = "/metrics"
	}
	return &Module{collector: collector, config: config, listen: net.Listen}
}

func (m *Module) ID() string             { return ModuleID }
func (m *Module) Dependencies() []string { return nil }

func (m *Module) Register(app *platformmodule.AppContext) error {
	if app == nil {
		return errors.New("observability module app context is required")
	}
	if err := app.Provide(PortMeter, metrics.Meter(m.collector)); err != nil {
		return err
	}
	return app.Provide(PortCollector, m.collector)
}

func (m *Module) Start(context.Context) error {
	if !m.config.PrometheusEnabled {
		return nil
	}
	if err := validateLoopbackAddress(m.config.PrometheusAddr); err != nil {
		return err
	}
	path := strings.TrimSpace(m.config.PrometheusPath)
	if !strings.HasPrefix(path, "/") || strings.ContainsAny(path, "?#") {
		return fmt.Errorf("invalid Prometheus path %q", path)
	}
	listener, err := m.listen("tcp", m.config.PrometheusAddr)
	if err != nil {
		return fmt.Errorf("listen for Prometheus exporter: %w", err)
	}
	server := &http.Server{
		Handler:           m.prometheusHandler(path),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	m.mu.Lock()
	m.listener = listener
	m.server = server
	m.serveErr = nil
	m.mu.Unlock()
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.mu.Lock()
			m.serveErr = err
			m.mu.Unlock()
		}
	}()
	return nil
}

func (m *Module) prometheusHandler(path string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(path, func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(m.collector.PrometheusText()))
	})
	return mux
}

func (m *Module) Stop(ctx context.Context) error {
	m.mu.RLock()
	server := m.server
	m.mu.RUnlock()
	if server == nil {
		return nil
	}
	if err := server.Shutdown(ctx); err != nil {
		return fmt.Errorf("stop Prometheus exporter: %w", err)
	}
	m.mu.Lock()
	m.server = nil
	m.listener = nil
	m.mu.Unlock()
	return nil
}

func (m *Module) Health(context.Context) platformmodule.Health {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.serveErr != nil {
		return platformmodule.Health{Status: platformmodule.HealthUnhealthy, Message: "Prometheus exporter stopped unexpectedly"}
	}
	if !m.config.PrometheusEnabled {
		return platformmodule.Health{Status: platformmodule.HealthHealthy, Message: "collection enabled; Prometheus exporter disabled"}
	}
	if m.listener == nil {
		return platformmodule.Health{Status: platformmodule.HealthDegraded, Message: "Prometheus exporter is not listening"}
	}
	return platformmodule.Health{Status: platformmodule.HealthHealthy, Message: "Prometheus exporter listening on loopback"}
}

func (m *Module) Addr() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.listener == nil {
		return ""
	}
	return m.listener.Addr().String()
}

func (m *Module) Collector() *metrics.Collector { return m.collector }

func validateLoopbackAddress(address string) error {
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return fmt.Errorf("invalid Prometheus address: %w", err)
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("Prometheus exporter must bind to an explicit loopback address")
	}
	return nil
}
