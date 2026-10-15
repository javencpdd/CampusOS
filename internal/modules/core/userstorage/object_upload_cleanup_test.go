package storage

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

type objectUploadAbortFailure struct {
	commitFailRepository
	ran   bool
	valid bool
}

func (r *objectUploadAbortFailure) Abort(ctx context.Context, id string, res reservation) error {
	r.ran = true
	deadline, ok := ctx.Deadline()
	r.valid = ctx.Err() == nil && ok && time.Until(deadline) <= 5*time.Second && id != "" && id == res.ObjectID
	if ctx.Err() != nil || !ok || time.Until(deadline) > 5*time.Second || id == "" || id != res.ObjectID {
		return errors.New("cleanup did not retain an independent bounded receipt")
	}
	return errors.New("actual pending receipt requires reconciliation")
}
func TestAttachmentUploadObjectAbortFailureIsExplicit(t *testing.T) {
	adapter, err := NewLocalAdapterWithQuota(t.TempDir(), 100)
	if err != nil {
		t.Fatal(err)
	}
	base := NewMemoryObjectRepository()
	repo := &objectUploadAbortFailure{commitFailRepository: commitFailRepository{base}}
	objects, err := NewObjectService(adapter, adapter, repo, 100)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	reader := &currentQuotaChangeReader{reader: bytes.NewReader([]byte("abc")), before: cancel}
	o, err := objects.Put(ctx, "19003", PutRequest{Namespace: "richtext", Purpose: "article_attachment", OriginalName: "x.pdf", SizeHint: 3, Reader: reader})
	if !errors.Is(err, ErrObjectCompensationPending) || o.ID != "" || !repo.ran || !repo.valid {
		t.Fatal("provider cleanup failure was hidden", o, err, repo.ran)
	}
	account, exists, err := base.Account(context.Background(), "19003")
	if err != nil || !exists || account.ReservedBytes != 3 || account.UsedBytes != 0 {
		t.Fatal("failed pending receipt disappeared", account, exists, err)
	}
	usage, err := adapter.Usage("19003")
	if err != nil || usage != 0 {
		t.Fatal("provider file was not removed", usage, err)
	}
}
