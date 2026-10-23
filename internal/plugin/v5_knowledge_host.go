package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
)

const (
	V5KnowledgeSearch     = "knowledge.public.search"
	V5KnowledgeSourceRead = "knowledge.public.source.read"
)

// V5PublicKnowledgeRead is bound by the composition root to the domain-owned
// current public Port. The plugin receives no repository, credential or ACL.
type V5PublicKnowledgeRead func(context.Context, string, json.RawMessage) (json.RawMessage, error)

func V5PublicKnowledgeEndpoint(code string, reader V5PublicKnowledgeRead, feature V5ChatFeatureReader) V5HostEndpoint {
	read := func(ctx context.Context, scope PluginV5Consume, raw json.RawMessage) (string, json.RawMessage, error) {
		if reader == nil || scope.Capability != code || scope.Scope.Kind != "collection" || (code != V5KnowledgeSearch && code != V5KnowledgeSourceRead) {
			return "", nil, ErrV5Denied
		}
		v, ok := v5JSON(raw)
		fields := map[string]v5Check{"collection_id": v5String(35, 35, `^kc_[0-9a-f]{32}$`)}
		required := "collection_id member_id revision"
		if code == V5KnowledgeSearch {
			required = "collection_id query page page_size"
			fields["query"] = v5String(1, 1024, `(?s)^.*$`)
			fields["page"] = v5Integer(1, 100000)
			fields["page_size"] = v5Integer(1, 50)
		} else {
			fields["member_id"] = v5String(35, 35, `^km_[0-9a-f]{32}$`)
			fields["revision"] = v5String(64, 64, `^[0-9a-f]{64}$`)
		}
		if !ok || !v5Object(required, fields)(v) {
			return "", nil, ErrV5CallInvalid
		}
		id := v.(map[string]any)["collection_id"].(string)
		if !v5Contains(scope.Scope.CollectionIDs, id) {
			return "", nil, ErrV5Denied
		}
		wire, err := reader(ctx, code, raw)
		if err != nil || ctx.Err() != nil || len(wire) > V5ChatMaxResponseBytes {
			return "", nil, ErrV5Denied
		}
		if _, ok := v5JSON(wire); !ok {
			return "", nil, ErrV5Denied
		}
		// Full current result, including ACL revision and projection generation, is
		// re-read by the Runtime guard and after the bounded call. No allow cache.
		return v5KnowledgeResultStamp(wire), wire, nil
	}
	return V5HostEndpoint{Feature: feature, FeatureID: "knowledge", ResultStamp: v5KnowledgeResultStamp, Authorize: func(ctx context.Context, _ string, s PluginV5Consume, r json.RawMessage) (string, error) {
		stamp, _, e := read(ctx, s, r)
		return stamp, e
	}, Call: func(ctx context.Context, _ string, s PluginV5Consume, r json.RawMessage, w io.Writer) error {
		_, wire, e := read(ctx, s, r)
		if e != nil {
			return e
		}
		_, e = w.Write(wire)
		return e
	}}
}

func v5KnowledgeResultStamp(raw json.RawMessage) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
