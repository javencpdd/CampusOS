package knowledge

import (
	"context"
	"errors"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

// IndexDiagnostics contains execution metadata only. Projection counts and a
// ready generation are never evidence that an original source is readable.
type IndexDiagnostics struct {
	CollectionID       string `json:"collection_id"`
	CollectionVersion  int64  `json:"collection_version,string"`
	JobVersion         int64  `json:"job_version,string"`
	State              string `json:"state"`
	Phase              string `json:"phase"`
	Attempts           int    `json:"attempts"`
	LastError          string `json:"last_error"`
	ReadyGeneration    int64  `json:"ready_generation,string"`
	RunGeneration      int64  `json:"run_generation,string"`
	AvailableAtMS      int64  `json:"available_at_ms,string"`
	Selections         int    `json:"selections"`
	ActiveSelections   int    `json:"active_selections"`
	Members            int    `json:"members"`
	ProjectedDocuments int    `json:"projected_documents"`
	ObservedAtMS       int64  `json:"observed_at_ms,string"`
	Retryable          bool   `json:"retryable"`
}

func (s *CollectionService) IndexDiagnostics(ctx context.Context, id string, resolve identityport.SessionPrincipalResolver) (IndexDiagnostics, error) {
	var out IndexDiagnostics
	if !opaque("kc_", id) {
		return out, ErrInvalid
	}
	store, ok := s.repo.(indexStore)
	if !ok {
		return out, ErrSourceUnavailable
	}
	err := s.adminRead(ctx, resolve, func(tx context.Context) error {
		// Match executor order before taking any member locks. A reader holding
		// members while waiting for a job can deadlock a page holding that job.
		if _, e := load[Collection](tx, s.repo, collections, id, false); e != nil {
			return e
		}
		j, jobErr := load[IndexJob](tx, s.repo, indexJobs, id, false)
		if jobErr != nil && !errors.Is(jobErr, ErrNotFound) {
			return jobErr
		}
		state, e := s.state(tx, id, false)
		if e != nil {
			return e
		}
		out = IndexDiagnostics{CollectionID: id, CollectionVersion: state.Collection.Version, State: "unconfigured", Selections: len(state.Selections), Members: len(state.Members), ObservedAtMS: time.Now().UnixMilli()}
		for _, rule := range state.Selections {
			if rule.Active {
				out.ActiveSelections++
			}
		}
		if jobErr == nil {
			out.JobVersion, out.State, out.Phase, out.Attempts = j.Version, j.State, j.Phase, j.Attempts
			out.ReadyGeneration, out.RunGeneration, out.AvailableAtMS = j.ReadyGeneration, j.RunGeneration, j.AvailableAtMS
			if j.LastError == "page_failed" || j.LastError == "lease_expired" {
				out.LastError = j.LastError
			}
			out.Retryable = j.State == "retry" || j.State == "dead"
		}
		out.ProjectedDocuments, e = store.indexDocumentCount(tx, id)
		return e
	})
	if err != nil {
		return IndexDiagnostics{}, err
	}
	return out, nil
}

// A manual retry is a fenced, CAS-protected command with the same current
// Admin/MFA/audit/Outbox boundary as source selection. It grants no read access.
func (s *CollectionService) RetryIndex(ctx context.Context, id string, collectionVersion, jobVersion int64, resolve identityport.SessionPrincipalResolver) error {
	if !opaque("kc_", id) || collectionVersion < 1 || jobVersion < 1 {
		return ErrInvalid
	}
	return s.managed(ctx, "index.retry", id, resolve, func(tx context.Context, _ string) error {
		c, e := load[Collection](tx, s.repo, collections, id, true)
		if e != nil {
			return e
		}
		if c.Version != collectionVersion {
			return ErrConflict
		}
		j, e := load[IndexJob](tx, s.repo, indexJobs, id, true)
		if e != nil {
			return e
		}
		if j.Version != jobVersion || (j.State != "retry" && j.State != "dead") {
			return ErrConflict
		}
		j.State, j.Attempts, j.LastError = "pending", 0, ""
		j.Requested++
		j.RunGeneration, j.ReadyGeneration, j.LeaseToken, j.LeaseUntilMS, j.AvailableAtMS = 0, 0, 0, 0, 0
		_, e = save(tx, s.repo, indexJobs, id, j.Version, j)
		return e
	})
}
