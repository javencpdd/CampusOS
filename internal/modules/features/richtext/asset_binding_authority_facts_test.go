package richtext

import (
	"context"
	"errors"
	"testing"
)

func TestAssetBindingAuthorityRequiresManagedStorage(t *testing.T) {
	var absent *PgStore
	for _, id := range []string{"", "0", "-1", "01", "+1", " 1", "9223372036854775808", "1"} {
		for _, lock := range []bool{false, true} {
			if raw, err := absent.LoadAssetAuthorityFacts(context.Background(), id, lock); raw != nil || !errors.Is(err, ErrAssetBindingFactsUnavailable) {
				t.Fatal(id, lock, raw, err)
			}
			if rows, err := absent.LoadArticleBindingAuthorityFacts(context.Background(), id, lock); rows != nil || !errors.Is(err, ErrAssetBindingFactsUnavailable) {
				t.Fatal(id, lock, rows, err)
			}
		}
	}
}
