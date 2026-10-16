package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	platformmodule "github.com/campusos/CampusOS/internal/platform/module"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"io"
	"os"
	"testing"
)

type imageUploadAuditFailure struct {
	reliability.Store
	mode string
}

func (s *imageUploadAuditFailure) RecordCommandAudit(ctx context.Context, a reliability.CommandAudit) error {
	if s.mode == "audit" {
		return errors.New("required image audit failed")
	}
	return s.Store.RecordCommandAudit(ctx, a)
}
func (s *imageUploadAuditFailure) Enqueue(ctx context.Context, e *reliability.Event) (*reliability.Event, error) {
	if s.mode == "outbox" {
		return nil, errors.New("required image event failed")
	}
	return s.Store.Enqueue(ctx, e)
}

type imageUploadReader struct {
	io.Reader
	calls int
	after func()
}

func (r *imageUploadReader) Read(p []byte) (int, error) {
	r.calls++
	n, err := r.Reader.Read(p)
	if r.after != nil {
		after := r.after
		r.after = nil
		after()
	}
	return n, err
}

type imageUploadStorageHook struct {
	Port
	after func()
}

func (s *imageUploadStorageHook) Path(owner string, parts ...string) (string, error) {
	path, err := s.Port.Path(owner, parts...)
	if s.after != nil {
		after := s.after
		s.after = nil
		after()
	}
	return path, err
}
func imageUploadPrincipal(_ context.Context) (identityport.PrincipalContext, error) {
	return identityport.PrincipalContext{Contract: identityport.PrincipalContract, Actor: identityport.PrincipalRef{Kind: "user", ID: "1001"}, Audience: "user", AuthenticationStrength: "password", CredentialID: "current-session"}, nil
}
func imageUploadFixture(t *testing.T, mode string) (*ContentImageStore, *LocalAdapter, *MemoryObjectRepository, *reliability.MemoryStore, *MemoryQuotaRepository) {
	t.Helper()
	quotas := NewMemoryQuotaRepository()
	adapter, err := NewLocalAdapterWithQuotaRepository(t.TempDir(), 1024*1024, quotas)
	if err != nil {
		t.Fatal(err)
	}
	objects := NewMemoryObjectRepository()
	account, err := NewObjectService(adapter, adapter, objects, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	images, err := NewContentImageStore(adapter, adapter, 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	events := reliability.NewMemoryStore()
	reliable := reliability.NewService(transaction.NewMemory(events), &imageUploadAuditFailure{Store: events, mode: mode})
	images.SetUploadCommand(reliable, account)
	return images, adapter, objects, events, quotas
}
func TestInlineImageUploadCoreCurrentProofQuotaAndAuditRollback(t *testing.T) {
	for _, mode := range []string{"current", "admin", "missing", "credential", "audit", "outbox", "quota_lower", "missing_account", "missing_audit", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s, adapter, objects, _, quotas := imageUploadFixture(t, mode)
			proof := identityport.SessionPrincipalResolver(imageUploadPrincipal)
			reader := &imageUploadReader{Reader: bytes.NewReader(testPNG(t))}
			switch mode {
			case "admin":
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, e := imageUploadPrincipal(ctx)
					p.Actor.Kind, p.Audience = "admin", "admin"
					return p, e
				}
			case "missing":
				proof = nil
			case "missing_account":
				s.account = nil
			case "missing_audit":
				s.reliable = nil
			case "canceled":
				cancel()
			case "credential":
				changed := false
				reader.after = func() { changed = true }
				proof = func(ctx context.Context) (identityport.PrincipalContext, error) {
					p, e := imageUploadPrincipal(ctx)
					if changed {
						p.CredentialID = "new-session"
					}
					return p, e
				}
			case "quota_lower":
				if _, err := quotas.Upsert(ctx, QuotaRecord{UserID: "1001", QuotaBytes: 1}); err != nil {
					t.Fatal(err)
				}
			}
			image, err := s.SaveForPrincipal(ctx, reader, proof)
			usage, e := adapter.Usage("1001")
			if e != nil {
				t.Fatal(e)
			}
			account, _, e := objects.Account(ctx, "1001")
			if e != nil {
				t.Fatal(e)
			}
			if mode == "current" {
				if err != nil || image == nil || usage != image.FileSize || account.UsedBytes != image.FileSize {
					t.Fatal(image, err, usage, account)
				}
			} else {
				if err == nil || image != nil || usage != 0 || account.UsedBytes != 0 {
					t.Fatal(mode, image, err, usage, account)
				}
			}
			if mode == "admin" || mode == "missing" || mode == "missing_account" || mode == "missing_audit" || mode == "canceled" {
				if reader.calls != 0 {
					t.Fatal("early denial read input", mode, reader.calls)
				}
			}
		})
	}
}
func TestInlineImageUploadCoreAfterWriteProofAndReceiptPending(t *testing.T) {
	for _, mode := range []string{"session", "strength", "cancel", "replace", "metadata"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s, adapter, objects, _, _ := imageUploadFixture(t, "")
			changed := false
			s.storage = &imageUploadStorageHook{Port: adapter, after: func() {
				changed = true
				if mode == "cancel" {
					cancel()
				}
			}}
			proof := func(ctx context.Context) (identityport.PrincipalContext, error) {
				p, e := imageUploadPrincipal(ctx)
				if changed {
					switch mode {
					case "session":
						return p, errors.New("session revoked")
					case "strength":
						p.AuthenticationStrength = "mfa"
					case "replace", "metadata":
						images, _ := s.ListOwned("1001")
						if len(images) > 0 {
							path, _ := adapter.Path("1001", ImageDir, ContentImageDir, images[0].FileName)
							if mode == "replace" {
								_ = os.Rename(path, path+".original")
								if err := os.WriteFile(path, []byte("other owned bytes"), 0o644); err != nil {
									t.Fatal(err)
								}
							} else {
								if err := os.WriteFile(path, bytes.Repeat([]byte("x"), int(images[0].FileSize)), 0o644); err != nil {
									t.Fatal(err)
								}
							}
							changed = false
						}
					}
				}
				return p, e
			}
			image, err := s.SaveForPrincipal(ctx, bytes.NewReader(testPNG(t)), proof)
			if err == nil || image != nil {
				t.Fatal("post-write mutation accepted", mode, image, err)
			}
			usage, _ := adapter.Usage("1001")
			ledger, _, _ := objects.Account(context.Background(), "1001")
			if ledger.UsedBytes != 0 {
				t.Fatal("failed allocation retained", ledger)
			}
			if mode == "replace" || mode == "metadata" {
				if !errors.Is(err, ErrInlineImageCleanupPending) || usage == 0 {
					t.Fatal("changed receipt not pending", mode, err, usage)
				}
			} else if usage != 0 {
				t.Fatal("exact receipt was not cleaned", mode, usage, err)
			}
		})
	}
}
func TestInlineImageUploadFilePrivateReceiptAndExactCleanup(t *testing.T) {
	ctx := context.Background()
	adapter, err := NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	file, err := PrepareInlineImage(ctx, bytes.NewReader(testPNG(t)), DefaultMaxContentImage)
	if err != nil {
		t.Fatal(err)
	}
	if err := transaction.NewMemory().Within(ctx, func(tx context.Context) error {
		return file.Write(tx, adapter, "1001", "content", ContentImageURLPrefix)
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(file)
	if err != nil || string(raw) != "{}" {
		t.Fatal("private receipt serialized", string(raw), err)
	}
	metadata := file.Metadata()
	metadata.FileName = "forged"
	metadata.FileSize = 0
	if err := file.Check(ctx); err != nil {
		t.Fatal("public alias changed private proof", err)
	}
	if err := file.Discard(); err != nil {
		t.Fatal(err)
	}
	if err := file.Discard(); err != nil {
		t.Fatal("own missing receipt retry", err)
	}
	if (&InlineImageFile{}).Check(ctx) == nil {
		t.Fatal("forged receipt accepted")
	}
}

func TestInlineImageUploadDefaultModuleWiringAndMissingCommandClosed(t *testing.T) {
	for _, mode := range []string{"current", "missing", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			app := platformmodule.NewAppContext()
			if mode == "current" {
				if err := app.Provide("platform.reliability.service", reliability.NewService(transaction.NewMemory(), reliability.NewMemoryStore())); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "invalid" {
				if err := app.Provide("platform.reliability.service", "incompatible"); err != nil {
					t.Fatal(err)
				}
			}
			module := NewModule(ModuleConfig{Root: t.TempDir(), QuotaBytes: 1024 * 1024})
			if err := module.Register(app); err != nil {
				t.Fatal(err)
			}
			err := module.Start(context.Background())
			if mode == "invalid" {
				if err == nil {
					t.Fatal("invalid production command adapter accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			image, err := module.ContentImages().SaveForPrincipal(context.Background(), bytes.NewReader(testPNG(t)), imageUploadPrincipal)
			if (err == nil) != (mode == "current") {
				t.Fatal(mode, image, err)
			}
			if mode == "missing" {
				if usage, _ := module.Adapter().Usage("1001"); usage != 0 {
					t.Fatal("missing command wrote files", usage)
				}
			}
		})
	}
}
