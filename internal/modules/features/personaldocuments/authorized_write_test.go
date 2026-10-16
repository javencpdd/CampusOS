package personaldocuments

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	corestorage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

type documentAuditFailure struct{ reliability.Store }

func (s documentAuditFailure) RecordCommandAudit(context.Context, reliability.CommandAudit) error {
	return errors.New("required document audit failed")
}

func TestDocumentAuthorCreateAndUpload(t *testing.T) {
	for _, operation := range []string{"create", "upload"} {
		for _, mode := range []string{"owner", "missing", "admin", "revoke", "audit", "disabled"} {
			t.Run(operation+"/"+mode, func(t *testing.T) {
				s, _ := newReliableTestService(t)
				store := reliability.NewMemoryStore()
				var durableStore reliability.Store = store
				if mode == "audit" {
					durableStore = documentAuditFailure{store}
				}
				s.SetReliability(reliability.NewService(transaction.NewMemory(store), durableStore))
				before := s.repository.(transaction.Snapshotter).Snapshot()
				proof := documentProof("1001")
				calls := 0
				resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
					calls++
					p, err := proof(ctx)
					if mode == "admin" {
						p.Actor.Kind = "admin"
						p.Audience = "admin"
						p.AuthenticationStrength = "mfa"
					}
					if mode == "revoke" && calls > 2 {
						return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
					}
					return p, err
				}
				if mode == "missing" {
					resolve = nil
				}
				if mode == "disabled" {
					s.SetEnabledChecker(func() bool { return false })
				}
				var d DocumentDetail
				var err error
				if operation == "create" {
					d, err = s.CreateTextForPrincipal(context.Background(), resolve, CreateRequest{Name: "private.txt", Format: FormatText, Content: "body"})
				} else {
					d, err = s.UploadForPrincipal(context.Background(), resolve, "private.pdf", FormatPDF, "forged", int64(len(safeTestPDF)), strings.NewReader(safeTestPDF))
				}
				if (err == nil) != (mode == "owner") {
					t.Fatal(d, err)
				}
				objects, e := s.objects.List(context.Background(), "1001", corestorage.ObjectFilter{}, corestorage.PageRequest{Limit: 100})
				if e != nil {
					t.Fatal(e)
				}
				if mode == "owner" {
					if d.OwnerID != "1001" || d.CurrentVersion == nil || len(objects.Items) != 1 {
						t.Fatal(d, objects)
					}
					return
				}
				if d.ID != "" || !reflect.DeepEqual(before, s.repository.(transaction.Snapshotter).Snapshot()) {
					t.Fatal("failed command persisted", d, err)
				}
				for _, o := range objects.Items {
					if o.Status != corestorage.ObjectStatusDeleted {
						t.Fatal("staged object not compensated", o, err)
					}
				}
				events, _, e := store.List(context.Background(), reliability.EventFilter{Limit: 100}, reliability.PageRequest{PageSize: 100})
				if e != nil || len(events) != 0 {
					t.Fatal("failed command outbox", events, e)
				}
			})
		}
	}
}

func runDocumentAuthorWrite(s *Service, ctx context.Context, proof DocumentPrincipalResolver, d DocumentDetail, selected, operation string) (DocumentDetail, error) {
	switch operation {
	case "save":
		return s.SaveForPrincipal(ctx, proof, d.ID, SaveRequest{ExpectedVersion: d.Version, Content: "next body"})
	case "restore_version":
		return s.RestoreVersionForPrincipal(ctx, proof, d.ID, selected, d.Version)
	case "trash":
		return s.SetStatusForPrincipal(ctx, proof, d.ID, d.Version, StatusTrashed)
	case "restore":
		return s.SetStatusForPrincipal(ctx, proof, d.ID, d.Version, StatusActive)
	}
	panic("unknown test operation")
}

func testDocumentAuthorWrite(t *testing.T, operation string) {
	for _, mode := range []string{"owner", "other", "admin", "missing", "stale", "wrong_state", "drift", "revoke", "audit", "disabled"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			s, _ := newReliableTestService(t)
			d, err := s.CreateText(ctx, "1001", CreateRequest{Name: "private.txt", Format: FormatText, Content: "original body"})
			if err != nil {
				t.Fatal(err)
			}
			selected := d.CurrentVersionID
			if operation == "restore" || mode == "wrong_state" && operation != "restore" {
				d, err = s.SetStatus(ctx, "1001", d.ID, d.Version, StatusTrashed)
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "wrong_state" && operation == "restore" {
				d, err = s.SetStatus(ctx, "1001", d.ID, d.Version, StatusActive)
				if err != nil {
					t.Fatal(err)
				}
			}
			store := reliability.NewMemoryStore()
			var port reliability.Store = store
			if mode == "audit" {
				port = documentAuditFailure{store}
			}
			s.SetReliability(reliability.NewService(transaction.NewMemory(store), port))
			initial := s.repository.(transaction.Snapshotter).Snapshot()
			proof := documentProof("1001")
			calls := 0
			resolve := func(ctx context.Context) (identityport.PrincipalContext, error) {
				calls++
				p, e := proof(ctx)
				if mode == "other" {
					p.Actor.ID = "1002"
				}
				if mode == "admin" {
					p.Actor.Kind = "admin"
					p.Audience = "admin"
					p.AuthenticationStrength = "mfa"
				}
				if mode == "revoke" && calls > 2 {
					return identityport.PrincipalContext{}, identityport.ErrUserSessionPrincipalUnavailable
				}
				if mode == "drift" && calls == 2 {
					r := s.repository.(*MemoryRepository)
					r.mu.Lock()
					v := r.docs[d.ID]
					v.Name = "changed.txt"
					r.docs[d.ID] = v
					r.mu.Unlock()
				}
				return p, e
			}
			if mode == "missing" {
				resolve = nil
			}
			if mode == "stale" {
				d.Version++
			}
			if mode == "disabled" {
				s.SetEnabledChecker(func() bool { return false })
			}
			result, err := runDocumentAuthorWrite(s, ctx, resolve, d, selected, operation)
			if (err == nil) != (mode == "owner") {
				t.Fatal(result, err)
			}
			if mode == "owner" {
				if result.Version != d.Version+1 {
					t.Fatal(result)
				}
				if operation == "trash" && (result.CurrentVersion != nil || result.CurrentVersionID != "") {
					t.Fatal("tombstone reference leaked")
				}
				if operation == "restore_version" {
					_, body, e := s.TextContent(ctx, "1001", d.ID)
					if e != nil || body != "original body" {
						t.Fatal(body, e)
					}
				}
				return
			}
			if result.ID != "" {
				t.Fatal("partial result", result)
			}
			// A drift injected before the command is an independently committed
			// change. Every other failure must leave the complete aggregate intact.
			if mode != "drift" && !reflect.DeepEqual(initial, s.repository.(transaction.Snapshotter).Snapshot()) {
				t.Fatal("partial document persisted", err)
			}
			objects, e := s.objects.List(ctx, "1001", corestorage.ObjectFilter{}, corestorage.PageRequest{Limit: 100})
			if e != nil {
				t.Fatal(e)
			}
			ready := 0
			for _, o := range objects.Items {
				if o.Status == corestorage.ObjectStatusReady {
					ready++
				}
			}
			if ready != 1 {
				t.Fatal("staged object not compensated", objects, err)
			}
			events, _, e := store.List(ctx, reliability.EventFilter{Limit: 100}, reliability.PageRequest{PageSize: 100})
			if e != nil || len(events) != 0 {
				t.Fatal("partial outbox", events, e)
			}
		})
	}
}

func TestDocumentAuthorSave(t *testing.T)           { testDocumentAuthorWrite(t, "save") }
func TestDocumentAuthorRestoreVersion(t *testing.T) { testDocumentAuthorWrite(t, "restore_version") }
func TestDocumentAuthorTrash(t *testing.T)          { testDocumentAuthorWrite(t, "trash") }
func TestDocumentAuthorRestore(t *testing.T)        { testDocumentAuthorWrite(t, "restore") }

type documentCancelObjects struct {
	corestorage.ObjectPort
	corestorage.OwnedObjectCommandLocker
	cancel context.CancelFunc
}

func (o *documentCancelObjects) Put(ctx context.Context, owner string, req corestorage.PutRequest) (corestorage.Object, error) {
	item, err := o.ObjectPort.Put(ctx, owner, req)
	if err == nil {
		o.cancel()
	}
	return item, err
}
func (o *documentCancelObjects) Delete(ctx context.Context, owner, id string, version int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return o.ObjectPort.Delete(ctx, owner, id, version)
}

func TestDocumentAuthorCancelledCommandCompensates(t *testing.T) {
	s, _ := newReliableTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.objects = &documentCancelObjects{ObjectPort: s.objects, OwnedObjectCommandLocker: s.objects.(corestorage.OwnedObjectCommandLocker), cancel: cancel}
	d, err := s.CreateTextForPrincipal(ctx, documentProof("1001"), CreateRequest{Name: "private.txt", Format: FormatText, Content: "body"})
	if !errors.Is(err, context.Canceled) || d.ID != "" {
		t.Fatal(d, err)
	}
	items, e := s.repository.List(context.Background(), "1001", ListFilter{})
	if e != nil || len(items) != 0 {
		t.Fatal(items, e)
	}
	objects, e := s.objects.List(context.Background(), "1001", corestorage.ObjectFilter{IncludeDeleted: true}, corestorage.PageRequest{Limit: 100})
	if e != nil || len(objects.Items) != 1 || objects.Items[0].Status != corestorage.ObjectStatusDeleted {
		t.Fatal("cancelled cleanup failed", objects, e)
	}
}
