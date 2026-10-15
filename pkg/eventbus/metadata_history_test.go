package eventbus

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type privateEventData struct{}

func (privateEventData) MarshalJSON() ([]byte, error) {
	panic("metadata history must never marshal event data")
}
func TestEventMetadataHistorySafeFilteredSnapshot(t *testing.T) {
	b := NewMemoryEventBus()
	ctx := context.Background()
	for i := 0; i < 120; i++ {
		e := NewEvent(EventThreadCreated, "secret-source", "private-subject", privateEventData{})
		e.Extensions = map[string]string{"token": "secret-token"}
		b.Publish(ctx, e)
	}
	b.Publish(ctx, NewEvent("private-type-secret", "secret", "subject", map[string]string{"body": "private-body"}))
	bad := NewEvent(EventThreadUpdated, "secret", "secret", nil)
	bad.ID = "private-id-secret"
	b.Publish(ctx, bad)
	rows, total := b.GetMetadataHistory(5, EventThreadCreated)
	if len(rows) != 5 || total != 120 {
		t.Fatal(rows, total)
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"secret", "subject", "data", "extensions", "token", "body"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private event fields leaked", string(raw))
		}
	}
	other, total := b.GetMetadataHistory(20, "other")
	if total != 1 || len(other) != 1 || other[0].Type != "other" || other[0].Source != "platform" {
		t.Fatal(other, total)
	}
	rows[0].ID = "caller-mutation"
	current, _ := b.GetMetadataHistory(5, EventThreadCreated)
	if current[0].ID == rows[0].ID {
		t.Fatal("metadata aliases stored history")
	}
	if rows, total := b.GetMetadataHistory(0, ""); len(rows) != 50 || total != 121 {
		t.Fatal("unbounded history result", len(rows), total)
	}
	if rows, total := b.GetMetadataHistory(5, EventCategoryCreated); len(rows) != 0 || total != 0 {
		t.Fatal(rows, total)
	}
}
