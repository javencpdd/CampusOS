package knowledge

import (
	"context"
	"errors"
	"github.com/campusos/CampusOS/pkg/observability"
	"testing"
	"time"
)

func TestIndexMetricsObserveActualPagesAndBoundedScheduleDelay(t *testing.T) {
	f, executor, c := indexFixture(t, 1)
	meter := observability.NewCollector()
	executor.meter = meter
	j := indexJob(t, f, c.ID)
	j.AvailableAtMS = time.Now().Add(-2 * time.Second).UnixMilli()
	if e := f.s.reliable.CheckedRead(t.Context(), func(tx context.Context) error { _, e := save(tx, f.repo, indexJobs, j.ID, j.Version, j); return e }); e != nil {
		t.Fatal(e)
	}
	indexDrain(t, f, executor, c.ID)
	seen := map[string]bool{}
	for _, metric := range meter.Snapshot().Metrics {
		switch metric.Name {
		case "campusos_knowledge_index_pages_total":
			if metric.Value < 1 || metric.Labels["result"] != "success" {
				t.Fatal(metric)
			}
			seen[metric.Name] = true
		case "campusos_knowledge_index_page_duration_seconds":
			if metric.Count < 1 || metric.Sum <= 0 {
				t.Fatal(metric)
			}
			seen[metric.Name] = true
		case "campusos_knowledge_index_schedule_lag_seconds":
			if metric.Count != 1 || metric.Sum < 2 || metric.Labels["state"] != "pending" {
				t.Fatal(metric)
			}
			seen[metric.Name] = true
		}
		if len(metric.Labels) > 1 {
			t.Fatal("unbounded or resource labels", metric)
		}
	}
	if len(seen) != 3 {
		t.Fatal(seen)
	}
	before := meter.Snapshot().Metrics
	if e := executor.runPage(t.Context()); e != nil {
		t.Fatal(e)
	}
	after := meter.Snapshot().Metrics
	// Empty polls are not execution pages or zero-latency samples.
	for i, m := range before {
		if m.Name == "campusos_knowledge_index_pages_total" && after[i].Value != m.Value {
			t.Fatal("empty poll counted")
		}
	}
	executor.store = indexMetricsFailedClaim{executor.store}
	if e := executor.runPage(t.Context()); !errors.Is(e, ErrSourceUnavailable) {
		t.Fatal("storage claim failure missing", e)
	}
	failed := false
	for _, metric := range meter.Snapshot().Metrics {
		if metric.Name == "campusos_knowledge_index_pages_total" && metric.Labels["result"] == "failure" && metric.Value == 1 {
			failed = true
		}
	}
	if !failed {
		t.Fatal("failed claim telemetry missing")
	}

}

type indexMetricsFailedClaim struct{ indexStore }

func (indexMetricsFailedClaim) selectIndexJob(context.Context, int64) (record, error) {
	return record{}, ErrSourceUnavailable
}
