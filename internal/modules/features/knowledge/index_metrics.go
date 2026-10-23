package knowledge

import (
	"github.com/campusos/CampusOS/pkg/observability"
	"time"
)

func (s *IndexService) recordScheduleLag(state string, due int64) {
	if s.meter == nil || due <= 0 {
		return
	}
	switch state {
	case "pending", "processing", "retry", "idle", "dead":
	default:
		return
	}
	_ = s.meter.Observe("campusos_knowledge_index_schedule_lag_seconds", observability.Labels{"state": state}, max(0, float64(s.now().UnixMilli()-due)/1000))
}
func (s *IndexService) recordPage(start time.Time, err error) {
	if s.meter == nil {
		return
	}
	result := "success"
	if err != nil {
		result = "failure"
	}
	labels := observability.Labels{"result": result}
	_ = s.meter.AddCounter("campusos_knowledge_index_pages_total", labels, 1)
	_ = s.meter.Observe("campusos_knowledge_index_page_duration_seconds", labels, time.Since(start).Seconds())
}
