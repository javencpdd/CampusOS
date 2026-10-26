package knowledge

import (
	"encoding/json"
	"testing"
)

func TestKnowledgeRecordEpochWinsOverPayload(t *testing.T) {
	row := record{Version: 7, Data: json.RawMessage(`{"id":"source","version":"900"}`)}
	c, e := decode[Collection](row)
	if e != nil || c.Version != 7 {
		t.Fatal(c, e)
	}
	m, e := decode[Member](row)
	if e != nil || m.Version != 7 {
		t.Fatal(m, e)
	}
	d, e := decode[IndexDocument](row)
	if e != nil || d.Version != 7 {
		t.Fatal(d, e)
	}
	j, e := decode[IndexJob](row)
	if e != nil || j.Version != 7 {
		t.Fatal(j, e)
	}
	o, e := decode[Official](row)
	if e != nil || o.Version != 7 {
		t.Fatal(o, e)
	}
	s, e := decode[Selection](row)
	if e != nil || s.Version != 7 {
		t.Fatal(s, e)
	}
	r, e := decode[OfficialRevision](row)
	if e != nil || r.Version != 7 {
		t.Fatal(r, e)
	}
	if _, e := decode[Collection](record{Data: json.RawMessage(`{"version":false}`)}); e == nil {
		t.Fatal("malformed stored epoch accepted")
	}
}
