package observability

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	metrics "github.com/campusos/CampusOS/pkg/observability"
)

func TestDisabledExporterKeepsCollectionHealthyWithoutListener(t *testing.T) {
	module := NewModule(metrics.NewCollector(), Config{})
	registry := platformmodule.NewRegistry(nil)
	if err := registry.Add(module, platformmodule.KindCore, true); err != nil {
		t.Fatal(err)
	}
	if err := registry.StartAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	if module.Addr() != "" {
		t.Fatalf("disabled exporter opened listener %s", module.Addr())
	}
	if health := module.Health(t.Context()); health.Status != platformmodule.HealthHealthy {
		t.Fatalf("health=%+v", health)
	}
	if err := registry.StopAll(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestExporterRejectsNonLoopbackAddress(t *testing.T) {
	module := NewModule(metrics.NewCollector(), Config{PrometheusEnabled: true, PrometheusAddr: "0.0.0.0:9091"})
	if err := module.Start(t.Context()); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Fatalf("non-loopback result=%v", err)
	}
}

func TestExporterLifecycleAndPrometheusResponse(t *testing.T) {
	collector := metrics.NewCollector()
	collector.RecordExternal("test.provider", true)
	module := NewModule(collector, Config{PrometheusEnabled: true, PrometheusAddr: "127.0.0.1:0", PrometheusPath: "/internal/metrics"})
	listener := newBlockingListener()
	module.listen = func(string, string) (net.Listener, error) { return listener, nil }
	registry := platformmodule.NewRegistry(nil)
	if err := registry.Add(module, platformmodule.KindCore, true); err != nil {
		t.Fatal(err)
	}
	if err := registry.StartAll(t.Context()); err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	module.prometheusHandler("/internal/metrics").ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/internal/metrics", nil))
	response := recorder.Result()
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), "campusos_external_requests_total") {
		t.Fatalf("status=%d body=%s", response.StatusCode, body)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := registry.StopAll(ctx); err != nil {
		t.Fatal(err)
	}
	if module.Addr() != "" {
		t.Fatalf("exporter still has address %s", module.Addr())
	}
}

type blockingListener struct {
	closed chan struct{}
	once   sync.Once
}

func newBlockingListener() *blockingListener { return &blockingListener{closed: make(chan struct{})} }

func (l *blockingListener) Accept() (net.Conn, error) {
	<-l.closed
	return nil, net.ErrClosed
}

func (l *blockingListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

func (*blockingListener) Addr() net.Addr { return testAddr("127.0.0.1:19091") }

type testAddr string

func (testAddr) Network() string  { return "tcp" }
func (a testAddr) String() string { return string(a) }
