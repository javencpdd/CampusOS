package plugin

import (
	"encoding/json"
	"math"
)

func v5ExtensionShape(point V5ContributionPoint, response bool) v5Check {
	fields := map[string]v5Check{"request_id": v5String(1, 128, `(?s)^.*$`), "contribution_id": v5String(1, 128, `(?s)^.*$`), "instance_generation": v5Integer(1, math.MaxInt64)}
	required := "request_id contribution_id instance_generation"
	add := func(k string, c v5Check) { fields[k] = c; required += " " + k }
	if !response {
		add("deadline_ms", v5Integer(1, 300000))
	}
	switch point {
	case V5PreviewRenderer:
		if response {
			add("ui_slot_id", v5String(1, 128, `(?s)^.*$`))
			add("resource_revision", v5String(1, 128, `(?s)^.*$`))
		} else {
			add("resource_ref", v5String(1, 128, `(?s)^.*$`))
			add("mime_type", v5Enum("application/pdf"))
		}
	case V5AIChat:
		if response {
			add("text", v5String(0, 50000, `(?s)^.*$`))
			add("input_tokens", v5Integer(0, math.MaxInt64))
			add("output_tokens", v5Integer(0, math.MaxInt64))
			add("finish_reason", v5Enum("completed", "length"))
		} else {
			add("prompt", v5String(1, 12000, `(?s)^.*$`))
			add("context_class", v5Enum("none", "public"))
		}
	case V5KnowledgeSource:
		if response {
			add("items", v5Array(0, 100, false, v5Object("external_id revision title text", map[string]v5Check{"external_id": v5String(1, 128, `(?s)^.*$`), "revision": v5String(1, 128, `(?s)^.*$`), "title": v5String(0, 500, `(?s)^.*$`), "text": v5String(0, 50000, `(?s)^.*$`), "source_url": v5String(1, 2048, `^https://`)})))
			fields["next_cursor"] = v5String(1, 128, `(?s)^.*$`)
		} else {
			add("binding_id", v5String(1, 128, `(?s)^.*$`))
			add("limit", v5Integer(1, 100))
			fields["cursor"] = v5String(1, 128, `(?s)^.*$`)
		}
	default:
		return func(any) bool { return false }
	}
	return v5Object(required, fields)
}

func parseV5Extension(point V5ContributionPoint, raw []byte, response bool) (map[string]any, error) {
	v, ok := v5JSON(raw)
	if !ok || !v5ExtensionShape(point, response)(v) {
		return nil, ErrV5CallInvalid
	}
	return v.(map[string]any), nil
}
func v5WireInt(v any) int64 {
	var n int64
	b, _ := json.Marshal(v5CanonicalNumbers(v))
	_ = json.Unmarshal(b, &n)
	return n
}
