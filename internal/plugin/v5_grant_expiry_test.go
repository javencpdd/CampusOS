package plugin

import (
	"context"
	"testing"
	"time"
)

func TestV5GrantPermanentAndExtendedDeadlines(t *testing.T) {
	for _, days := range []int64{0, 7, 30, 90, 180, 365, 3650} {
		t.Run(v5Counter(days)+"-days", func(t *testing.T) {
			v := newV5TestPlatform(t)
			v.approved(t)
			ctx := context.Background()
			before, _ := v.s.grant(ctx, v.c)
			now := v.s.now()
			expiry := int64(0)
			if days > 0 {
				expiry = now.Add(time.Duration(days) * 24 * time.Hour).UnixMilli()
			}
			v.mutate(t, V5Mutation{Kind: "grant", Approved: true, ExpectedRevision: before.Revision, ExpiresAtMS: expiry, IdempotencyKey: "extend-expiry"}, true, v.admin)
			grant, err := v.s.grant(ctx, v.c)
			if err != nil || grant.ExpiresAtMS != expiry || grant.Revision <= before.Revision {
				t.Fatal("expiry not persisted with a new revision", err)
			}
			if !v5GrantActive(grant, now.UnixMilli()) {
				t.Fatal("new grant inactive")
			}
			if expiry == 0 {
				if !v5GrantActive(grant, now.AddDate(100, 0, 0).UnixMilli()) {
					t.Fatal("permanent grant unexpectedly expired")
				}
			} else if v5GrantActive(grant, expiry) {
				t.Fatal("finite expiry did not revoke access")
			}
			if _, err := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "grant", Version: v.c.Version, Approved: true, ExpectedRevision: before.Revision, ExpiresAtMS: expiry, IdempotencyKey: "stale-extension"}, true, v.admin); err != ErrV5Conflict {
				t.Fatal("stale extension bypassed CAS", err)
			}
			v.mutate(t, V5Mutation{Kind: "revoke", ExpectedRevision: grant.Revision, IdempotencyKey: "revoke-extended"}, true, v.admin)
			revoked, _ := v.s.grant(ctx, v.c)
			if v5GrantActive(revoked, now.UnixMilli()) {
				t.Fatal("extended/permanent grant survived revocation")
			}
		})
	}
}

func TestV5GrantRejectsPastOrUnsupportedDeadlines(t *testing.T) {
	v := newV5TestPlatform(t)
	v.approved(t)
	ctx := context.Background()
	now := v.s.now()
	v.s.now = func() time.Time { return now }
	before, _ := v.s.grant(ctx, v.c)
	for _, expiry := range []int64{-1, now.UnixMilli() - 1, now.UnixMilli(), v5MaxGrantExpiryMS + 1} {
		_, err := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "grant", Version: v.c.Version, Approved: true, ExpectedRevision: before.Revision, ExpiresAtMS: expiry, IdempotencyKey: "bad-expiry-" + v5Counter(expiry)}, true, v.admin)
		if err != ErrV5Invalid {
			t.Fatal("invalid deadline accepted", expiry, err)
		}
		after, _ := v.s.grant(ctx, v.c)
		if after.Revision != before.Revision || after.ExpiresAtMS != before.ExpiresAtMS {
			t.Fatal("rejected expiry changed saved grant")
		}
	}
}
