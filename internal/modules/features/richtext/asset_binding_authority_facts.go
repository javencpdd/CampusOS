package richtext

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

var ErrAssetBindingFactsUnavailable = errors.New("asset and attachment facts unavailable")

// Complete internal storage snapshots; these do not authenticate a caller or
// authorize an attachment write. The owning business command supplies scope.
type AssetBindingAuthorityFact struct {
	Binding json.RawMessage
	Asset   json.RawMessage
}

type AssetBindingAuthorityReader interface {
	CheckAssetBindingAuthority(context.Context) error
	CheckAssetReferenceAuthority(context.Context) error
	LoadAssetAuthorityFacts(context.Context, string, bool) (json.RawMessage, error)
	LoadArticleBindingAuthorityFacts(context.Context, string, bool) ([]AssetBindingAuthorityFact, error)
}

// Asset row FOR UPDATE protects an empty reference inventory only while
// both immediate, validated FKs still enforce KEY SHARE on that row.
func (s *PgStore) CheckAssetReferenceAuthority(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return ErrAssetBindingFactsUnavailable
	}
	var valid bool
	err := s.db(ctx).QueryRow(ctx, `SELECT count(*)=2 AND count(DISTINCT c.conrelid)=2 FROM
 (VALUES ('public.richtext_article_attachments'),('public.richtext_article_assets')) x(tbl)
 JOIN pg_constraint c ON c.conrelid=to_regclass(x.tbl)
 WHERE c.contype='f' AND c.confrelid=to_regclass('public.user_assets')
 AND c.conkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid=c.conrelid AND attname='asset_id' AND NOT attisdropped)]::smallint[]
 AND c.confkey=ARRAY[(SELECT attnum FROM pg_attribute WHERE attrelid=c.confrelid AND attname='id' AND NOT attisdropped)]::smallint[]
 AND c.convalidated AND NOT c.condeferrable
 AND (SELECT count(*)>=2 AND bool_and(tgenabled IN('O','A')) FROM pg_trigger WHERE tgconstraint=c.oid AND tgisinternal)`).Scan(&valid)
	if err != nil || !valid {
		return ErrAssetBindingFactsUnavailable
	}
	return nil
}

func (s *PgStore) CheckAssetBindingAuthority(ctx context.Context) error {
	if s == nil || s.pool == nil {
		return ErrAssetBindingFactsUnavailable
	}
	return s.requireAssetBindingFacts(ctx)
}

func (s *PgStore) requireAssetBindingFacts(ctx context.Context) error {
	var managed bool
	err := s.db(ctx).QueryRow(ctx, `SELECT
 (SELECT count(*)=2 FROM (VALUES
 ('public.user_assets','trg_user_assets_richtext_facts_version'),
 ('public.richtext_article_attachments','trg_article_attachments_richtext_facts_version')
 ) x(tbl,trg) JOIN pg_trigger t ON t.tgrelid=to_regclass(x.tbl) AND t.tgname=x.trg
 WHERE NOT t.tgisinternal AND t.tgenabled IN('O','A') AND t.tgtype=23
 AND t.tgfoid=to_regprocedure('public.advance_richtext_asset_binding_facts_version()'))
 AND EXISTS(SELECT 1 FROM pg_sequence WHERE seqrelid=to_regclass('public.richtext_asset_binding_facts_version_seq')
 AND NOT seqcycle AND seqtypid='bigint'::regtype AND seqincrement=1)
 AND (SELECT count(*)=2 FROM pg_attribute WHERE attrelid IN
 (to_regclass('public.user_assets'),to_regclass('public.richtext_article_attachments'))
 AND attname='facts_version' AND attnotnull AND atttypid='bigint'::regtype AND NOT attisdropped)`).Scan(&managed)
	if err != nil || !managed {
		return ErrAssetBindingFactsUnavailable
	}
	return nil
}

func (s *PgStore) LoadAssetAuthorityFacts(ctx context.Context, id string, lock bool) (json.RawMessage, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if s == nil || s.pool == nil || err != nil || n <= 0 || strconv.FormatInt(n, 10) != id {
		return nil, ErrAssetBindingFactsUnavailable
	}
	if lock {
		if _, ok := transaction.FromContext(ctx); !ok {
			return nil, ErrAssetBindingFactsUnavailable
		}
	}
	if err := s.requireAssetBindingFacts(ctx); err != nil {
		return nil, err
	}
	query := `SELECT to_jsonb(u) FROM public.user_assets u WHERE u.id=$1`
	if lock {
		query += " FOR UPDATE"
	}
	var raw []byte
	err = s.db(ctx).QueryRow(ctx, query, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAssetNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := s.requireAssetBindingFacts(ctx); err != nil {
		return nil, err
	}
	return append(json.RawMessage(nil), raw...), nil
}

func (s *PgStore) LoadArticleBindingAuthorityFacts(ctx context.Context, article string, lock bool) ([]AssetBindingAuthorityFact, error) {
	n, err := strconv.ParseInt(article, 10, 64)
	if s == nil || s.pool == nil || err != nil || n <= 0 || strconv.FormatInt(n, 10) != article {
		return nil, ErrAssetBindingFactsUnavailable
	}
	if lock {
		if _, ok := transaction.FromContext(ctx); !ok {
			return nil, ErrAssetBindingFactsUnavailable
		}
		// Serialize binding admission, including an empty inventory, before
		// selecting row facts. Business commands must also lock their parent
		// and recheck the complete aggregate before committing.
		if _, err := s.db(ctx).Exec(ctx, attachmentAdmissionLockSQL, article); err != nil {
			return nil, err
		}
	}
	if err := s.requireAssetBindingFacts(ctx); err != nil {
		return nil, err
	}
	query := `SELECT to_jsonb(a),to_jsonb(u) FROM public.richtext_article_attachments a
 JOIN public.user_assets u ON u.id=a.asset_id
 WHERE a.article_content_id=$1 ORDER BY a.display_order,a.id`
	if lock {
		query += " FOR UPDATE OF a,u"
	}
	rows, err := s.db(ctx).Query(ctx, query, article)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	facts := []AssetBindingAuthorityFact{}
	for rows.Next() {
		var binding, asset []byte
		if err := rows.Scan(&binding, &asset); err != nil {
			return nil, err
		}
		facts = append(facts, AssetBindingAuthorityFact{Binding: append(json.RawMessage(nil), binding...), Asset: append(json.RawMessage(nil), asset...)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if err := s.requireAssetBindingFacts(ctx); err != nil {
		return nil, err
	}
	return facts, nil
}
