package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/campusos/CampusOS/internal/modules/core/community/domain"
)

// PrivateIndexFacts is metadata only, not a read grant. Current User Policy is
// mandatory before obtaining any title/body or using a saved derivative.

func (r *PgThreadRepository) PrivateIndexFacts(ctx context.Context, id string) (domain.PrivateIndexFacts, error) {
	var f domain.PrivateIndexFacts
	var kind, deletion, moderation, board, parent, categoryJSON, parentJSON string
	e := r.db(ctx).QueryRow(ctx, `SELECT t.author_id::text,t.content_format,t.facts_version,octet_length(t.content),t.thread_type,t.deletion_status,t.moderation_status,c.lifecycle_status,p.lifecycle_status,jsonb_build_object('id',c.id,'parent_id',c.parent_id,'node_kind',c.node_kind,'lifecycle_status',c.lifecycle_status,'version',c.version)::text,jsonb_build_object('id',p.id,'parent_id',p.parent_id,'node_kind',p.node_kind,'lifecycle_status',p.lifecycle_status,'version',p.version)::text FROM threads t JOIN categories c ON c.id=t.category_id JOIN categories p ON p.id=c.parent_id WHERE t.id=$1::bigint`, id).Scan(&f.Owner, &f.Format, &f.Epoch, &f.Bytes, &kind, &deletion, &moderation, &board, &parent, &categoryJSON, &parentJSON)
	if e != nil {
		return f, e
	}
	f.Available = kind == "discussion" && deletion == "active" && moderation == "clear" && board == "active" && parent == "active"
	raw, _ := json.Marshal([]any{id, f.Owner, f.Epoch, f.Format, kind, deletion, moderation, categoryJSON, parentJSON})
	sum := sha256.Sum256(raw)
	f.Revision = hex.EncodeToString(sum[:])
	return f, nil
}
