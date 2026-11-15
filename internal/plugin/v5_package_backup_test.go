package plugin

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

func v5BackupFixture(t *testing.T) (v5TestPlatform, V5Installation) {
	t.Helper()
	v, target := v5CheckpointFixture(t)
	ctx := context.Background()
	user := v5CheckpointCurrent(t, v)
	user.Owner = "1001"
	user.Generation, _ = v.store.next(ctx)
	user.Revision, _ = v.store.next(ctx)
	if err := v.store.save(ctx, v5User, v5StateID(v.c, user.Owner), user); err != nil {
		t.Fatal(err)
	}
	v5CheckpointWrite(t, v, user, "state", "retained private data")
	current := v5CheckpointCurrent(t, v)
	op, err := v.s.Mutate(ctx, target.Publisher, target.Key, V5Mutation{Kind: "upgrade", Version: target.Version, ExpectedGeneration: current.Generation, IdempotencyKey: "switch-global"}, true, v.admin)
	if err != nil {
		t.Fatal(err)
	}
	v.ready(t, op)
	return v, user
}

func TestV5PackageRepositoryPreservesReferencedDataWithoutOldExecution(t *testing.T) {
	v, user := v5BackupFixture(t)
	ctx := context.Background()
	before := v5CheckpointCurrent(t, v)
	op := v.mutate(t, V5Mutation{Kind: "purge_release", IdempotencyKey: "archive-old"}, true, v.admin)
	v.process(t)
	if v.operation(t, op.ID).Status != "done" {
		t.Fatal("archive did not complete")
	}
	r, _ := v.s.release(ctx, v.c)
	if r.Storage != "purged" {
		t.Fatal("old version still in hot catalog", r.Storage)
	}
	if _, err := v.s.files.Package(r.Digest); err != ErrV5NotFound {
		t.Fatal("old executable archive retained", err)
	}
	raw, err := v.s.files.RepositoryPackage(r)
	if err != nil || !bytes.Equal(raw, v.raw) {
		t.Fatal("compressed backup changed", err)
	}
	if after := v5CheckpointCurrent(t, v); after != before {
		t.Fatal("current installation changed")
	}
	var after V5Installation
	if err := v.store.load(ctx, v5User, v5StateID(v.c, user.Owner), &after); err != nil || !reflect.DeepEqual(after, user) {
		t.Fatal("user installation changed", err)
	}
	checkpoint, err := v.s.captureCheckpoint(ctx, user, v5RandomID())
	if err != nil {
		t.Fatal("user can no longer upgrade from archived version", err)
	}
	body, err := v.s.checkpoint(ctx, user, checkpoint)
	if err != nil || string(body.Files["state"]) != "retained private data" {
		t.Fatal("private data lost", err)
	}
	if _, err := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "enable", Version: v.c.Version, ExpectedGeneration: before.Generation, IdempotencyKey: "no-revive"}, true, v.admin); err != ErrV5PlatformUnavailable {
		t.Fatal("backup revived execution", err)
	}
	v.s.Users = &v5TestUsers{active: true}
	upgrade, err := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "upgrade", Version: before.Version, ExpectedGeneration: user.Generation, IdempotencyKey: "user-upgrade-from-repository"}, false, v5TestProof("user", user.Owner))
	if err != nil {
		t.Fatal("user upgrade from repository rejected", err)
	}
	v.ready(t, upgrade)
	updated, err := v.s.installation(ctx, v.c, user.Owner)
	if err != nil || updated.Version != before.Version {
		t.Fatal("user did not reach current version", err)
	}
}

type v5BackupAuditFault struct {
	reliability.Store
	phase, failAt int
}

func (f *v5BackupAuditFault) RecordCommandAudit(ctx context.Context, audit reliability.CommandAudit) error {
	if audit.CommandCode == "plugin.v5.worker.purge_release" {
		f.phase++
		if f.phase == f.failAt {
			return errors.New("injected repository phase audit failure")
		}
	}
	return f.Store.RecordCommandAudit(ctx, audit)
}

func TestV5PackageRepositoryAuditRecoveryAndReimportFence(t *testing.T) {
	for _, mode := range []string{"backup-audit", "cleanup-audit", "reimport"} {
		t.Run(mode, func(t *testing.T) {
			v, user := v5BackupFixture(t)
			ctx := context.Background()
			before := v5CheckpointCurrent(t, v)
			op := v.mutate(t, V5Mutation{Kind: "purge_release", IdempotencyKey: "archive-fault"}, true, v.admin)
			failure := &v5BackupAuditFault{Store: v.fault.Store, failAt: 2}
			if mode == "backup-audit" {
				failure.failAt = 1
			}
			v.s.reliable = reliability.NewService(transaction.NewMemory(v.store, v.fault.Store.(transaction.Snapshotter)), failure)
			v.process(t)
			failed := v.operation(t, op.ID)
			if failed.Status != "failed" {
				t.Fatal("audit failure not retained", failed.Status)
			}
			r, _ := v.s.release(ctx, v.c)
			if mode == "backup-audit" {
				if r.Storage != "available" {
					t.Fatal("failed backup changed storage")
				}
				if _, err := v.s.files.Package(r.Digest); err != nil {
					t.Fatal("backup audit failure deleted original", err)
				}
			} else {
				if r.Storage != "purged" {
					t.Fatal("archive intent not committed")
				}
				if _, err := v.s.captureCheckpoint(ctx, user, v5RandomID()); err != nil {
					t.Fatal("cleanup failure broke data recovery", err)
				}
			}
			if mode == "reimport" {
				if _, err := v.s.Import(ctx, v.raw, v.envelope, "reimport-before-cleanup", v.admin); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := v.s.Mutate(ctx, v.c.Publisher, v.c.Key, V5Mutation{Kind: "retry", Version: v.c.Version, OperationID: op.ID, ExpectedRevision: failed.Revision, IdempotencyKey: "retry-archive"}, true, v.admin); err != nil {
				t.Fatal(err)
			}
			v.process(t)
			status := v.operation(t, op.ID).Status
			if mode == "reimport" {
				if status != "superseded" {
					t.Fatal("reimport did not fence old deletion", status)
				}
				if _, err := v.s.files.Package(r.Digest); err != nil {
					t.Fatal("reimported package deleted", err)
				}
			} else if status != "done" {
				t.Fatal("retry did not finish", status)
			}
			if after := v5CheckpointCurrent(t, v); after != before {
				t.Fatal("archive failure affected active version")
			}
		})
	}
}

func TestV5PackageRepositoryRejectsUnsafeOrChangedBackups(t *testing.T) {
	for _, mode := range []string{"package-conflict", "metadata-conflict", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			v, _ := v5BackupFixture(t)
			ctx := context.Background()
			r, _ := v.s.release(ctx, v.c)
			if err := v.s.files.BackupPackage(r); err != nil {
				t.Fatal(err)
			}
			d, err := v.s.files.directory("repository/" + r.Publisher + "/" + r.Key + "/" + r.Version)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			name := "package.tar.gz"
			if mode == "metadata-conflict" {
				name = "metadata.json"
			}
			if err := d.Remove(name); err != nil {
				t.Fatal(err)
			}
			if mode == "symlink" {
				outside := t.TempDir() + "/keep"
				if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := d.Symlink(outside, name); err != nil {
					t.Fatal(err)
				}
			} else {
				file, err := d.OpenFile(name, os.O_CREATE|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = file.Write([]byte("changed"))
				file.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			before := v5CheckpointCurrent(t, v)
			op := v.mutate(t, V5Mutation{Kind: "purge_release", IdempotencyKey: "reject-backup"}, true, v.admin)
			v.process(t)
			if v.operation(t, op.ID).Status != "failed" {
				t.Fatal("unsafe backup accepted")
			}
			if _, err := v.s.files.Package(r.Digest); err != nil {
				t.Fatal("source removed before backup verification", err)
			}
			if after := v5CheckpointCurrent(t, v); after != before {
				t.Fatal("backup error changed current version")
			}
		})
	}
}
