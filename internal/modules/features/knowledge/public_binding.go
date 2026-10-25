package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// PublicBindingRevision exposes only a current public binding proof to trusted
// composition. Returned source bytes still require Source on every use.
func (s *CollectionService) PublicBindingRevision(ctx context.Context, id string) (string, error) {
	if s == nil || !opaque("kc_", id) || s.check(ctx) != nil {
		return "", ErrSourceUnavailable
	}
	var stamp string
	e := s.reliable.CheckedRead(ctx, func(tx context.Context) error {
		state, e := s.state(tx, id, false)
		if e != nil || state.Collection.Status != "active" {
			return ErrSourceUnavailable
		}
		wire, e := json.Marshal(state)
		if e != nil {
			return e
		}
		sum := sha256.Sum256(wire)
		stamp = hex.EncodeToString(sum[:])
		return nil
	})
	return stamp, e
}
