package plugin

import (
	"context"
	"os"
	"testing"
)

func TestV5UpgradeFromQuarantinedReleasePreservesDataWithoutUnquarantining(t *testing.T) {
	v, target := v5CheckpointFixture(t)
	ctx := context.Background()
	current := v5CheckpointCurrent(t, v)
	v5CheckpointWrite(t, v, current, "state", "existing user data")
	old, err := v.s.release(ctx, v.c)
	if err != nil {
		t.Fatal(err)
	}
	old.Storage = "quarantined"
	if err = v.store.save(ctx, v5Release, v.c.id(), old); err != nil {
		t.Fatal(err)
	}
	op, err := v.s.Mutate(ctx, target.Publisher, target.Key, V5Mutation{Kind: "upgrade", Version: target.Version, ExpectedGeneration: current.Generation, IdempotencyKey: "recover-data-only"}, true, v.admin)
	if err != nil || op.Checkpoint.ID == "" {
		t.Fatal("compatible trusted upgrade could not snapshot quarantined data", err)
	}
	body, err := v.s.checkpoint(ctx, current, op.Checkpoint)
	if err != nil || string(body.Files["state"]) != "existing user data" {
		t.Fatal("private data not preserved", err)
	}
	v.ready(t, op)
	after, err := v.s.installation(ctx, target, "")
	if err != nil || after.Version != target.Version {
		t.Fatal("target did not activate", err)
	}
	stored, _ := v.s.release(ctx, v.c)
	if stored.Storage != "quarantined" || stored.Digest != old.Digest {
		t.Fatal("source identity or quarantine changed")
	}
}

func TestV5QuarantinedUpgradeRequiresIntactSourceArchive(t *testing.T) {
	for _, kind := range []string{"missing", "digest-mismatch", "purged"} {
		t.Run(kind, func(t *testing.T) {
			v, target := v5CheckpointFixture(t)
			ctx := context.Background()
			current := v5CheckpointCurrent(t, v)
			old, err := v.s.release(ctx, v.c)
			if err != nil {
				t.Fatal(err)
			}
			old.Storage = "quarantined"
			switch kind {
			case "missing":
				if err = v.s.files.RemovePackage(old.Digest); err != nil {
					t.Fatal(err)
				}
			case "digest-mismatch":
				file, err := v.s.files.root.OpenFile("packages/"+old.Digest[7:]+".tar.gz", os.O_WRONLY|os.O_TRUNC, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = file.Write([]byte("not the recorded package"))
				file.Close()
				if err != nil {
					t.Fatal(err)
				}
			case "purged":
				old.Storage = "purged"
			}
			if err = v.store.save(ctx, v5Release, v.c.id(), old); err != nil {
				t.Fatal(err)
			}
			_, err = v.s.Mutate(ctx, target.Publisher, target.Key, V5Mutation{Kind: "upgrade", Version: target.Version, ExpectedGeneration: current.Generation, IdempotencyKey: "unsafe-source"}, true, v.admin)
			if err != ErrV5Rollback {
				t.Fatal("unverifiable source accepted", err)
			}
			after, _ := v.s.installation(ctx, v.c, "")
			if after != current {
				t.Fatal("rejected upgrade changed active state")
			}
		})
	}
}
