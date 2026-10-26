package ai

import (
	"context"
	"errors"

	"github.com/campusos/CampusOS/internal/platform/security"
	"github.com/campusos/CampusOS/internal/platform/transaction"
	"github.com/jackc/pgx/v5"
)

const profileColumns = `id,revision,definition,model,endpoint,fingerprint,secret_ref,enabled,timeout_ms,max_tokens,max_concurrent`

func (s *ManagedService) profile(ctx context.Context, id string, lock bool) (ManagedProfile, error) {
	var p ManagedProfile
	q := `SELECT ` + profileColumns + ` FROM ai_model_profiles WHERE id=$1`
	if lock {
		q += ` FOR UPDATE`
	}
	e := transaction.ExecutorFor(ctx, s.pool).QueryRow(ctx, q, id).Scan(&p.ID, &p.Revision, &p.Definition, &p.Model, &p.Endpoint, &p.Fingerprint, &p.SecretRef, &p.Enabled, &p.TimeoutMS, &p.MaxTokens, &p.MaxConcurrent)
	return p, e
}
func (s *ManagedService) binding(ctx context.Context, lock bool) (ManagedBinding, error) {
	var b ManagedBinding
	q := `SELECT name,profile_id,revision FROM ai_model_bindings WHERE name='chat'`
	if lock {
		q += ` FOR UPDATE`
	}
	e := transaction.ExecutorFor(ctx, s.pool).QueryRow(ctx, q).Scan(&b.Name, &b.ProfileID, &b.Revision)
	return b, e
}
func (s *ManagedService) budget(ctx context.Context, id string, lock bool) (ManagedBudget, error) {
	var b ManagedBudget
	q := `SELECT profile_id,quota,charged,reserved FROM ai_model_budgets WHERE profile_id=$1`
	if lock {
		q += ` FOR UPDATE`
	}
	e := transaction.ExecutorFor(ctx, s.pool).QueryRow(ctx, q, id).Scan(&b.ProfileID, &b.Quota, &b.Charged, &b.Reserved)
	return b, e
}
func (s *ManagedService) run(ctx context.Context, id, owner string, lock bool) (ManagedRun, error) {
	var r ManagedRun
	q := `SELECT id,owner_id,profile_id,profile_revision,binding_revision,instance_id,state,lease_until_ms,charged_units,usage_known,prompt_tokens,completion_tokens,total_tokens,error_code,created_at FROM ai_model_runs WHERE id=$1 AND ($2='' OR owner_id=$2)`
	if lock {
		q += ` FOR UPDATE`
	}
	e := transaction.ExecutorFor(ctx, s.pool).QueryRow(ctx, q, id, owner).Scan(&r.ID, &r.Owner, &r.ProfileID, &r.ProfileRevision, &r.BindingRevision, &r.InstanceID, &r.State, &r.Lease, &r.Charged, &r.UsageKnown, &r.Usage.PromptTokens, &r.Usage.CompletionTokens, &r.Usage.TotalTokens, &r.ErrorCode, &r.CreatedAt)
	r.Contract = ManagedContractVersion
	return r, e
}
func (s *ManagedService) events(ctx context.Context, id string) ([]ManagedEvent, error) {
	r, e := s.run(ctx, id, "", false)
	if e != nil {
		return nil, e
	}
	rows, e := transaction.ExecutorFor(ctx, s.pool).Query(ctx, `SELECT ordinal,kind FROM ai_model_run_events WHERE run_id=$1 ORDER BY ordinal`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ManagedEvent{}
	for rows.Next() {
		v := ManagedEvent{Contract: ManagedEventsVersion, RunID: id}
		if e = rows.Scan(&v.Ordinal, &v.Kind); e != nil {
			return nil, e
		}
		if v.Kind == "usage" {
			known := r.UsageKnown
			v.UsageKnown = &known
			v.ChargedUnits = r.Charged
			if known {
				usage := r.Usage
				v.Usage = &usage
			}
		}
		if v.Ordinal == 3 {
			v.ErrorCode = r.ErrorCode
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *ManagedService) lockedSnapshot(ctx context.Context, p ManagedProfile, b ManagedBinding) error {
	current, e := s.binding(ctx, true)
	if e != nil {
		return e
	}
	if current != b {
		return ErrManagedConflict
	}
	currentProfile, e := s.profile(ctx, p.ID, true)
	if e != nil {
		return e
	}
	if currentProfile != p || !currentProfile.Enabled {
		return ErrManagedConflict
	}
	return s.check(ctx)
}
func (s *ManagedService) current(ctx context.Context, p ManagedProfile, b ManagedBinding) error {
	if e := s.check(ctx); e != nil {
		return e
	}
	current, e := s.binding(ctx, false)
	if e != nil {
		return e
	}
	if current != b {
		return ErrManagedConflict
	}
	cp, e := s.profile(ctx, p.ID, false)
	if e != nil {
		return e
	}
	if cp != p || !cp.Enabled {
		return ErrManagedConflict
	}
	return nil
}
func (s *ManagedService) secret(ctx context.Context, p ManagedProfile) ([]byte, error) {
	var env security.Envelope
	var profile, endpoint, purpose string
	e := transaction.ExecutorFor(ctx, s.pool).QueryRow(ctx, `SELECT profile_id,endpoint,purpose,key_id,nonce,ciphertext FROM ai_model_secrets WHERE id=$1`, p.SecretRef).Scan(&profile, &endpoint, &purpose, &env.KeyID, &env.Nonce, &env.Ciphertext)
	if e != nil {
		return nil, ErrManagedUnavailable
	}
	if profile != p.ID || endpoint != p.Endpoint || purpose != "chat" {
		return nil, ErrManagedDenied
	}
	env.Algorithm = security.AlgorithmAES256GCM
	plain, e := s.keys.Open(env, managedSecretAAD(p, p.SecretRef))
	if e != nil {
		return nil, ErrManagedUnavailable
	}
	return plain, nil
}
func (s *ManagedService) provision(ctx context.Context) error {
	return s.command(ctx, "profiles.provision", "system", "ai-gateway", "profiles", func(tx context.Context) error {
		db := transaction.ExecutorFor(tx, s.pool)
		// A single transaction and advisory lock serialize first deployment across
		// processes without resetting the persisted binding or spent budget.
		if _, e := db.Exec(tx, `SELECT pg_advisory_xact_lock(hashtextextended('ai.managed.provision',0))`); e != nil {
			return e
		}
		if _, e := db.Exec(tx, `SELECT name FROM ai_model_bindings WHERE name='chat' FOR UPDATE`); e != nil {
			return e
		}
		for _, d := range s.configs {
			p := d.Profile
			existing, e := s.profile(tx, p.ID, true)
			if e != nil && !errors.Is(e, pgx.ErrNoRows) {
				return e
			}
			if e == nil && existing.Fingerprint == p.Fingerprint {
				continue
			}
			p.SecretRef = managedID("as_")
			env, e := s.keys.Seal([]byte(d.Key), managedSecretAAD(p, p.SecretRef))
			if e != nil {
				return ErrManagedUnavailable
			}
			_, e = db.Exec(tx, `INSERT INTO ai_model_profiles(id,definition,model,endpoint,fingerprint,secret_ref,enabled,timeout_ms,max_tokens,max_concurrent) VALUES($1,$2,$3,$4,$5,$6,true,$7,$8,$9) ON CONFLICT(id) DO UPDATE SET revision=nextval('ai_gateway_epoch_seq'),definition=excluded.definition,model=excluded.model,endpoint=excluded.endpoint,fingerprint=excluded.fingerprint,secret_ref=excluded.secret_ref,timeout_ms=excluded.timeout_ms,max_tokens=excluded.max_tokens,max_concurrent=excluded.max_concurrent`, p.ID, p.Definition, p.Model, p.Endpoint, p.Fingerprint, p.SecretRef, p.TimeoutMS, p.MaxTokens, p.MaxConcurrent)
			if e != nil {
				return e
			}
			_, e = db.Exec(tx, `INSERT INTO ai_model_secrets(id,profile_id,key_id,nonce,ciphertext,endpoint,purpose) VALUES($1,$2,$3,$4,$5,$6,'chat')`, p.SecretRef, p.ID, env.KeyID, env.Nonce, env.Ciphertext, p.Endpoint)
			if e != nil {
				return e
			}
			_, e = db.Exec(tx, `INSERT INTO ai_model_budgets(profile_id,quota) VALUES($1,$2) ON CONFLICT(profile_id) DO UPDATE SET quota=excluded.quota`, p.ID, d.Quota)
			if e != nil {
				return e
			}
		}
		_, e := db.Exec(tx, `INSERT INTO ai_model_bindings(name,profile_id) VALUES('chat',$1) ON CONFLICT(name) DO NOTHING`, s.defaultProfile)
		return e
	})
}

// Settlement is separate from caller/session cancellation. It never repeats
// external work. Expired dispatched requests conservatively cost one unit.
func (s *ManagedService) settle(ctx context.Context, id, state, code string, usage TokenUsage) (ManagedRun, error) {
	var out ManagedRun
	e := s.command(ctx, "call.settle", "worker", "ai-gateway", id, func(tx context.Context) error {
		if _, e := s.binding(tx, true); e != nil {
			return e
		}
		r, e := s.run(tx, id, "", false)
		if e != nil {
			return e
		}
		if _, e = s.profile(tx, r.ProfileID, true); e != nil {
			return e
		}
		if _, e = s.budget(tx, r.ProfileID, true); e != nil {
			return e
		}
		r, e = s.run(tx, id, "", true)
		if e != nil {
			return e
		}
		if r.State != "reserved" && r.State != "dispatched" {
			out = r
			return nil
		}
		if r.Lease <= managedNow() {
			state = "unknown"
			code = "expired"
			usage = TokenUsage{}
		}
		charged := 0
		if r.State == "dispatched" {
			charged = 1
		}
		if r.State == "reserved" && state == "completed" {
			return ErrManagedConflict
		}
		if state != "completed" && charged == 1 {
			state = "unknown"
			usage = TokenUsage{}
		}
		known := state == "completed" && usage.TotalTokens > 0
		terminal := "failed"
		if state == "completed" {
			terminal = "completed"
		}
		if code == "cancelled" || code == "deadline" {
			terminal = "cancelled"
		}
		db := transaction.ExecutorFor(tx, s.pool)
		tag, e := db.Exec(tx, `UPDATE ai_model_budgets SET reserved=reserved-1,charged=charged+$2 WHERE profile_id=$1 AND reserved>0`, r.ProfileID, charged)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			return ErrManagedBudget
		}
		_, e = db.Exec(tx, `UPDATE ai_model_runs SET state=$2,charged_units=$3,usage_known=$4,prompt_tokens=$5,completion_tokens=$6,total_tokens=$7,error_code=$8 WHERE id=$1`, id, state, charged, known, usage.PromptTokens, usage.CompletionTokens, usage.TotalTokens, code)
		if e != nil {
			return e
		}
		_, e = db.Exec(tx, `INSERT INTO ai_model_run_events(run_id,ordinal,kind) VALUES($1,2,'usage'),($1,3,$2)`, id, terminal)
		if e != nil {
			return e
		}
		out, e = s.run(tx, id, "", false)
		return e
	})
	if e != nil {
		return ManagedRun{}, e
	}
	out.Events, e = s.events(ctx, id)
	return out, e
}
