package richtext

import (
	"testing"
	"time"
)

func TestContentAdmissionIsBoundedAndReleases(t *testing.T) {
	limiter := contentAdmission{}
	now := time.Now()
	var releases []func()
	for i := 0; i < maxUserContentStreams; i++ {
		release, _ := limiter.acquire("user", now)
		if release == nil {
			t.Fatal("allowed stream rejected")
		}
		releases = append(releases, release)
	}
	if release, retry := limiter.acquire("user", now); release != nil || retry != 1 {
		t.Fatal("user concurrency bound bypassed")
	}
	for _, release := range releases {
		release()
		release()
	}
	if limiter.active != 0 || limiter.users["user"].active != 0 {
		t.Fatal("stream slots leaked")
	}
	for i := maxUserContentStreams; i < maxUserContentRequestsPerMinute; i++ {
		release, _ := limiter.acquire("user", now)
		if release == nil {
			t.Fatal("early rate rejection")
		}
		release()
	}
	if release, retry := limiter.acquire("user", now); release != nil || retry < 1 {
		t.Fatal("request rate bound bypassed")
	}
	release, _ := limiter.acquire("user", now.Add(time.Minute))
	if release == nil {
		t.Fatal("window did not recover")
	}
	release()
}

func TestInvocationRetentionPreservesRecentFallbackAndBoundsBatch(t *testing.T) {
	store := NewMemoryStore()
	now := time.Now().UTC()
	for _, id := range []string{"old1", "old2", "old3"} {
		store.invocations[id] = &PluginUIInvocation{ID: id, ExpiresAt: now.Add(-48 * time.Hour)}
	}
	store.invocations["recent"] = &PluginUIInvocation{ID: "recent", ExpiresAt: now.Add(-time.Hour)}
	count, err := store.PruneInvocations(t.Context(), now.Add(-24*time.Hour), 2)
	if err != nil || count != 2 || store.invocations["recent"] == nil || len(store.invocations) != 2 {
		t.Fatalf("invalid prune result: %d %v", count, err)
	}
}

func TestInvocationDigestSurvivesMemoryRollback(t *testing.T) {
	store := NewMemoryStore()
	store.invocations["one"] = &PluginUIInvocation{ID: "one", ContextDigest: "snapshot-only"}
	snapshot := store.Snapshot()
	store.invocations["one"].ContextDigest = "changed"
	store.Restore(snapshot)
	if store.invocations["one"].ContextDigest != "snapshot-only" {
		t.Fatal("rollback lost private invocation digest")
	}
}
