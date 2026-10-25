package knowledge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"reflect"
	"sync"
	"time"
)

const PortPluginPublicSource = "knowledge.plugin-public-source/v1"

type PluginSourceRequest struct {
	Publisher, Key, Version, ContributionID, RequestID, CollectionID, Query string
	Page, Limit                                                             int
	TimeoutMS                                                               int64
}
type PluginSourceItem struct{ ExternalID, Revision, Title, Text, SourceURL string }
type PluginSourceResult struct {
	Items      []PluginSourceItem
	NextCursor string
}
type PluginSourcePage struct {
	Items    []VisibleSource
	NextPage int
}
type PublicPluginSourceAdapter interface {
	ListPublicPlugin(context.Context, identity.SessionPrincipalResolver, PluginSourceRequest, func(context.Context) (string, error)) (PluginSourceResult, error)
}
type PluginPublicSource interface {
	List(context.Context, identity.SessionPrincipalResolver, PluginSourceRequest) (PluginSourcePage, error)
}
type pluginSourceService struct {
	collections *CollectionService
	adapter     PublicPluginSourceAdapter
	mu          sync.Mutex
	lifecycle   sync.Mutex
	life        context.Context
	cancel      context.CancelFunc
	calls       sync.WaitGroup
}

func PluginSourceCursor(query string, page int) string {
	w, _ := json.Marshal(struct {
		Query string `json:"query"`
		Page  int    `json:"page"`
	}{query, page})
	return base64.RawURLEncoding.EncodeToString(w)
}
func newPluginSourceService(c *CollectionService, a PublicPluginSourceAdapter) *pluginSourceService {
	ctx, cancel := context.WithCancel(context.Background())
	return &pluginSourceService{collections: c, adapter: a, life: ctx, cancel: cancel}
}
func (s *pluginSourceService) start() {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.life.Err() != nil {
		s.life, s.cancel = context.WithCancel(context.Background())
	}
}
func (s *pluginSourceService) stop() {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	s.mu.Lock()
	s.cancel()
	s.mu.Unlock()
	s.calls.Wait()
}
func (s *pluginSourceService) List(ctx context.Context, resolve identity.SessionPrincipalResolver, q PluginSourceRequest) (PluginSourcePage, error) {
	zero := PluginSourcePage{}
	if s == nil || s.collections == nil || nilSourceAdapter(s.adapter) || resolve == nil || !opaque("kc_", q.CollectionID) || !textValid(q.Query, 64) || q.Page < 1 || q.Page > 100000 || q.Limit < 1 || q.Limit > 20 || q.TimeoutMS < 1 || q.TimeoutMS > 5000 || !textValid(q.Publisher, 128) || !textValid(q.Key, 128) || !textValid(q.Version, 64) || !textValid(q.ContributionID, 128) || !textValid(q.RequestID, 128) || len(PluginSourceCursor(q.Query, q.Page)) > 128 {
		return zero, ErrSourceUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(q.TimeoutMS)*time.Millisecond)
	defer cancel()
	s.mu.Lock()
	if s.life.Err() != nil {
		s.mu.Unlock()
		return zero, ErrSourceUnavailable
	}
	s.calls.Add(1)
	life := s.life
	s.mu.Unlock()
	defer s.calls.Done()
	stop := context.AfterFunc(life, cancel)
	defer stop()
	p, e := resolve(ctx)
	if e != nil || p.Validate() != nil || p.Actor.Kind != "user" || p.Audience != "user" {
		return zero, ErrRAGDenied
	}
	bound := func(c context.Context) (identity.PrincipalContext, error) {
		now, e := resolve(c)
		if e != nil || c.Err() != nil || !reflect.DeepEqual(now, p) {
			return identity.PrincipalContext{}, ErrRAGDenied
		}
		return now, nil
	}
	stamp, e := s.collections.PublicBindingRevision(ctx, q.CollectionID)
	if e != nil {
		return zero, e
	}
	guard := func(c context.Context) (string, error) {
		if _, e := bound(c); e != nil {
			return "", e
		}
		current, e := s.collections.PublicBindingRevision(c, q.CollectionID)
		if e != nil || current != stamp {
			return "", ErrRAGChanged
		}
		return current, nil
	}
	if _, e = guard(ctx); e != nil {
		return zero, e
	}
	result, e := s.adapter.ListPublicPlugin(ctx, bound, q, guard)
	if e != nil || len(result.Items) > q.Limit {
		return zero, ErrSourceUnavailable
	}
	out := PluginSourcePage{Items: []VisibleSource{}}
	if result.NextCursor != "" {
		if q.Page == 100000 || result.NextCursor != PluginSourceCursor(q.Query, q.Page+1) {
			return zero, ErrSourceUnavailable
		}
		out.NextPage = q.Page + 1
	}
	seen := map[string]bool{}
	for _, item := range result.Items {
		if seen[item.ExternalID] || !opaque("km_", item.ExternalID) {
			return zero, ErrSourceUnavailable
		}
		seen[item.ExternalID] = true
		source, e := s.collections.Source(ctx, q.CollectionID, item.ExternalID, item.Revision)
		if e != nil || source.Source.Title != item.Title || source.Source.Content != item.Text || (item.SourceURL != "" && item.SourceURL != source.Source.URL) {
			return zero, ErrSourceUnavailable
		}
		out.Items = append(out.Items, source)
	}
	// Reopen the current primary source after every untrusted result has been
	// checked. Neither the plugin nor a persisted projection can assert an ACL.
	for _, item := range out.Items {
		now, e := s.collections.Source(ctx, q.CollectionID, item.MemberID, item.Revision)
		if e != nil || !reflect.DeepEqual(now, item) {
			return zero, ErrSourceUnavailable
		}
	}
	if _, e = guard(ctx); e != nil {
		return zero, e
	}
	return out, nil
}
