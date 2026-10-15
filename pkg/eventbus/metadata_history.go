package eventbus

import (
	"time"

	"github.com/google/uuid"
)

// EventMetadata deliberately has no subject, arbitrary source, extensions or
// payload. A metadata permission never grants access to the original resource.
type EventMetadata struct {
	ID     string `json:"id"`
	Type   string `json:"type"`
	Source string `json:"source"`
	Time   string `json:"time"`
}

func MetadataEventType(value string) string {
	switch value {
	case EventUserCreated, EventUserUpdated, EventThreadCreated, EventThreadUpdated, EventThreadDeleted, EventPostCreated, EventPostDeleted, EventCategoryCreated, EventCategoryUpdated, EventCategoryMoved, EventCategoryArchived, EventCategoryRestored:
		return value
	default:
		return "other"
	}
}

// Filtering precedes count and limit; both come from the same locked snapshot.
// It never touches Data, including values with unsafe JSON marshal methods.
func (b *MemoryEventBus) GetMetadataHistory(limit int, filter string) ([]EventMetadata, int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	rows := []EventMetadata{}
	for _, e := range b.history {
		if len(e.ID) != 36 || len(e.Time) > 64 {
			continue
		}
		id, err := uuid.Parse(e.ID)
		if err != nil {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, e.Time)
		if err != nil || at.IsZero() {
			continue
		}
		kind := MetadataEventType(e.Type)
		if filter != "" && kind != filter {
			continue
		}
		source := "community"
		switch kind {
		case EventUserCreated, EventUserUpdated:
			source = "identity"
		case "other":
			source = "platform"
		}
		rows = append(rows, EventMetadata{ID: id.String(), Type: kind, Source: source, Time: at.UTC().Format(time.RFC3339Nano)})
	}
	total := len(rows)
	if limit < 1 || limit > 100 {
		limit = 50
	}
	if total > limit {
		rows = rows[total-limit:]
	}
	return rows, total
}
