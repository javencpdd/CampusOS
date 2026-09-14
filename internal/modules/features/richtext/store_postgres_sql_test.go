package richtext

import (
	"strings"
	"testing"
)

// String IDs are intentional at the HTTP/domain boundary to preserve int64
// precision in JavaScript. Keep the PostgreSQL attachment-admission query
// explicit, because $2 is also used as text for the advisory-lock key.
func TestCreateAttachmentSQLCastsStringIDsForBigintColumns(t *testing.T) {
	for _, fragment := range []string{
		"SELECT $1::bigint, $2::bigint, $3::bigint",
		"WHERE a.article_content_id=$2::bigint",
		"bool_or(a.asset_id=$3::bigint)",
	} {
		if !strings.Contains(createAttachmentSQL, fragment) {
			t.Fatalf("attachment SQL must retain %q to bridge string API IDs to PostgreSQL bigint columns", fragment)
		}
	}
}

func TestPluginUIInvocationSQLSupportsExplicitOwnerAssetContext(t *testing.T) {
	for _, fragment := range []string{
		"context_kind",
		"NULLIF($6,'')::bigint",
		"COALESCE(article_content_id::text,'')",
	} {
		if !strings.Contains(createInvocationSQL+getInvocationSQL, fragment) {
			t.Fatalf("plugin UI invocation SQL must retain %q for nullable personal-asset context", fragment)
		}
	}
}
