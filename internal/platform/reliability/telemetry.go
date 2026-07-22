package reliability

import (
	"strings"
	"time"

	"github.com/campusos/CampusOS/pkg/observability"
)

type telemetry struct {
	meter observability.Meter
}

func (t telemetry) operation(operation, result string) {
	if t.meter == nil {
		return
	}
	_ = t.meter.AddCounter("campusos_reliability_operations_total", observability.Labels{
		"operation": safeMetricValue(operation),
		"result":    safeMetricValue(result),
	}, 1)
}

func (t telemetry) consumer(consumer, result string, duration time.Duration) {
	if t.meter == nil {
		return
	}
	_ = t.meter.Observe("campusos_reliability_consumer_duration_seconds", observability.Labels{
		"consumer": safeMetricValue(consumer),
		"result":   safeMetricValue(result),
	}, duration.Seconds())
}

func (t telemetry) queue(summary Summary) {
	if t.meter == nil {
		return
	}
	for status, value := range map[string]int64{
		StatusPending:    summary.Pending,
		StatusProcessing: summary.Processing,
		StatusPublished:  summary.Published,
		StatusRetry:      summary.Retry,
		StatusDead:       summary.Dead,
	} {
		_ = t.meter.SetGauge("campusos_reliability_queue_events", observability.Labels{"status": status}, float64(value))
	}
	_ = t.meter.SetGauge("campusos_reliability_oldest_pending_age_seconds", nil, summary.OldestPendingAgeSeconds)
}

func safeMetricValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	if strings.ContainsAny(value, "@/\\?=&%\n\r\t ") {
		return "unknown"
	}
	var result strings.Builder
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || strings.ContainsRune("._:-", char) {
			result.WriteRune(char)
		} else {
			result.WriteByte('_')
		}
		if result.Len() >= 96 {
			break
		}
	}
	if result.Len() == 0 {
		return "unknown"
	}
	return result.String()
}
