package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync/atomic"
	"time"
	"unicode/utf8"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/platform/transaction"
)

var errV5Replay = errors.New("internal v5 operation replay")

// Mutation carries intent only. Principal, owner, current grant, desired state,
// lease and observed health are always supplied/reloaded by the host.
type V5Mutation struct {
	Kind               string                     `json:"kind"`
	Version            string                     `json:"version"`
	ExpectedGeneration int64                      `json:"expected_generation,string"`
	ExpectedRevision   int64                      `json:"expected_revision,string"`
	IdempotencyKey     string                     `json:"idempotency_key"`
	Approved           bool                       `json:"approved"`
	Consumes           []PluginV5Consume          `json:"consumes,omitempty"`
	Resources          *PluginV5ManifestResources `json:"host_resources,omitempty"`
	Accepted           bool                       `json:"accepted"`
	PurposeHash        string                     `json:"purpose_hash,omitempty"`
	Values             json.RawMessage            `json:"values,omitempty"`
	SecretRefs         map[string]string          `json:"secret_refs,omitempty"`
	ConfigurationPart  string                     `json:"configuration_part,omitempty"`
	Reason             string                     `json:"reason,omitempty"`
	ExpiresAtMS        int64                      `json:"expires_at_ms,string,omitempty"`
	Purge              bool                       `json:"purge_data"`
	OperationID        string                     `json:"operation_id,omitempty"`
}
type V5Platform struct {
	managed  atomic.Pointer[V5ManagedRuntime]
	store    V5StateStore
	files    *V5ManagedFiles
	catalog  *PluginV5PackageCatalog
	reliable *reliability.Service
	admins   identity.AdminExecutionAuthorityReader
	Users    identity.UserReader
	now      func() time.Time
	// Compiled host adapters may bind current selectors/Secret references. An
	// absent adapter never turns a declaration or opaque string into permission.
	VerifySelector  func(context.Context, V5Coordinate, string, string, string) (bool, error)
	VerifySecretRef func(context.Context, V5Coordinate, string, string, string) error
	ConfigResources V5ConfigResourceReader
}

func NewV5Platform(store V5StateStore, files *V5ManagedFiles, catalog *PluginV5PackageCatalog, reliable *reliability.Service, admins identity.AdminExecutionAuthorityReader) *V5Platform {
	s := &V5Platform{store: store, files: files, catalog: catalog, reliable: reliable, admins: admins, now: time.Now}
	if reliable != nil {
		if snapshot, ok := store.(transaction.Snapshotter); ok {
			reliable.RegisterMemorySnapshotters(snapshot)
		}
	}
	return s
}

type v5Actor struct {
	p          identity.PrincipalContext
	authority  identity.AdminExecutionAuthority
	admin      bool
	permission string
}

func v5Permission(kind string) string {
	switch kind {
	case "read":
		return "plugin.plugin.read"
	case "import", "install", "upgrade", "purge_release":
		return "plugin.plugin.install"
	case "grant", "revoke", "configure", "consent":
		return "plugin.plugin.configure"
	case "uninstall":
		return "plugin.plugin.uninstall"
	default:
		return "plugin.plugin.lifecycle"
	}
}
func v5AdminQuery(code string) identity.AdminExecutionQuery {
	return identity.AdminExecutionQuery{PermissionCode: code, Scope: identity.PolicyResourceRef{Kind: "plugin.installation_inventory", ID: "all"}}
}
func (s *V5Platform) actor(ctx context.Context, admin bool, kind string, resolve identity.SessionPrincipalResolver, lock bool) (v5Actor, error) {
	a := v5Actor{admin: admin, permission: v5Permission(kind)}
	if s == nil || s.store == nil || s.files == nil || s.catalog == nil || s.reliable == nil || resolve == nil {
		return a, ErrV5PlatformUnavailable
	}
	if admin {
		if s.admins == nil {
			return a, ErrV5PlatformUnavailable
		}
		q := v5AdminQuery(a.permission)
		var err error
		a.authority, err = s.admins.LoadAdminExecutionAuthority(ctx, q, resolve, lock)
		if err != nil {
			return a, err
		}
		a.p = a.authority.Principal
		if identity.CheckAdminExecutionAuthority(a.authority, a.authority) != nil || a.authority.Query != q || !a.authority.Allowed || s.now().UnixMilli() >= a.authority.ExpiresAtMS {
			return a, ErrV5Denied
		}
	} else {
		p, err := resolve(ctx)
		if err != nil {
			return a, ErrV5Denied
		}
		a.p = p
	}
	kindExpected := "user"
	if admin {
		kindExpected = "admin"
	}
	if a.p.Validate() != nil || a.p.Actor.Kind != kindExpected || a.p.Audience != kindExpected || !admin && !v5OwnerValid(a.p.Actor.ID) {
		return a, ErrV5Denied
	}
	return a, nil
}
func (s *V5Platform) recheck(ctx context.Context, a v5Actor, kind string, resolve identity.SessionPrincipalResolver) error {
	b, err := s.actor(ctx, a.admin, kind, resolve, true)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(a, b) {
		return ErrV5Conflict
	}
	return ctx.Err()
}
func (s *V5Platform) release(ctx context.Context, c V5Coordinate) (V5PackageRelease, error) {
	var r V5PackageRelease
	if !c.valid() {
		return r, ErrV5Invalid
	}
	err := s.store.load(ctx, v5Release, c.id(), &r)
	return r, err
}
func (s *V5Platform) installation(ctx context.Context, c V5Coordinate, owner string) (V5Installation, error) {
	var i V5Installation
	k := v5Global
	if owner != "" {
		k = v5User
	}
	err := s.store.load(ctx, k, v5StateID(c, owner), &i)
	return i, err
}
func (s *V5Platform) saveInstall(ctx context.Context, i *V5Installation) error {
	n, err := s.store.next(ctx)
	if err != nil {
		return err
	}
	i.Revision = n
	k := v5Global
	if i.Owner != "" {
		k = v5User
	}
	return s.store.save(ctx, k, v5StateID(i.coordinate(), i.Owner), i)
}
func (s *V5Platform) saveOp(ctx context.Context, op *V5Operation) error {
	n, err := s.store.next(ctx)
	if err != nil {
		return err
	}
	op.Revision = n
	return s.store.save(ctx, v5Operation, op.ID, op)
}
func (s *V5Platform) grant(ctx context.Context, c V5Coordinate) (V5Grant, error) {
	var g V5Grant
	err := s.store.load(ctx, v5Grant, c.id(), &g)
	return g, err
}
func V5PurposeHash(m *PluginV5Manifest) string {
	raw, _ := json.Marshal(m.Consumes)
	return v5BytesDigest(raw)
}
func v5RequiredConsent(m *PluginV5Manifest) bool {
	for _, c := range m.Consumes {
		if c.Scope.Kind == "self" {
			return true
		}
	}
	return false
}
func v5GrantValid(m *PluginV5Manifest, g V5Grant) bool {
	if g.Consumes == nil || len(g.Consumes) > len(m.Consumes) {
		return false
	}
	seen := map[string]bool{}
	for _, c := range g.Consumes {
		raw, _ := json.Marshal(c)
		if seen[string(raw)] {
			return false
		}
		seen[string(raw)] = true
		found := false
		for _, decl := range m.Consumes {
			if reflect.DeepEqual(c, decl) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if g.Resources != nil {
		r := m.HostResources
		a := g.Resources
		if r == nil || a.CPUMillis < 1 || a.CPUMillis > r.CPUMillis || a.MemoryMB < 1 || a.MemoryMB > r.MemoryMB || a.StorageBytes < 1 || a.StorageBytes > r.StorageBytes || a.MaxConcurrency < 1 || a.MaxConcurrency > r.MaxConcurrency || a.TimeoutMS < 1 || a.TimeoutMS > r.TimeoutMS {
			return false
		}
		seen := map[string]bool{}
		for _, target := range a.NetworkTargets {
			if seen[target] {
				return false
			}
			seen[target] = true
			found := false
			for _, d := range r.NetworkTargets {
				if target == d {
					found = true
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}
func v5GrantActive(g V5Grant, now int64) bool {
	return g.Approved && (g.ExpiresAtMS == 0 || g.ExpiresAtMS > now)
}
func v5GrantComplete(m *PluginV5Manifest, g V5Grant) bool {
	return v5GrantCompleteAt(m, g, time.Now().UnixMilli())
}
func v5GrantCompleteAt(m *PluginV5Manifest, g V5Grant, now int64) bool {
	if !v5GrantActive(g, now) || !v5GrantValid(m, g) {
		return false
	}
	for _, c := range m.Consumes {
		if c.Required {
			found := false
			for _, a := range g.Consumes {
				if reflect.DeepEqual(a, c) {
					found = true
				}
			}
			if !found {
				return false
			}
		}
	}
	return m.HostResources == nil || g.Resources != nil
}
func (s *V5Platform) consentOK(ctx context.Context, c V5Coordinate, owner string, m *PluginV5Manifest, g V5Grant) bool {
	if owner == "" || !v5RequiredConsent(m) {
		return true
	}
	var consent V5Consent
	return s.store.load(ctx, v5Consent, v5VersionOwnerID(c, owner), &consent) == nil && consent.Accepted && consent.PurposeHash == V5PurposeHash(m) && consent.GrantRevision == g.Revision
}
func (s *V5Platform) validateConfig(ctx context.Context, c V5Coordinate, owner string, m *PluginV5Manifest, cfg V5Configuration) error {
	return s.validateConfigPart(ctx, c, owner, m, cfg, "")
}
func (s *V5Platform) validateConfigPart(ctx context.Context, c V5Coordinate, owner string, m *PluginV5Manifest, cfg V5Configuration, part string) error {
	raw, err := json.Marshal(m)
	if err != nil {
		return ErrV5Invalid
	}
	d, err := parsePluginV5ConfigDefinition(raw)
	if err != nil {
		return err
	}
	if err = validatePluginV5Values(d, cfg.Values); err != nil {
		return err
	}
	if err = validatePluginV5SecretRefs(d, cfg.SecretRefs); err != nil {
		return err
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(cfg.Values, &values) != nil {
		return ErrV5Invalid
	}
	for _, selector := range d.Selectors {
		if part == "secret_refs" {
			continue
		}
		v, ok := values[selector.Field]
		if !ok {
			continue
		}
		var value string
		if json.Unmarshal(v, &value) != nil {
			return ErrV5Invalid
		}
		allowed, err := s.configResourceAllowed(ctx, c, owner, selector.Kind, "", value)
		if s.VerifySelector != nil {
			allowed, err = s.VerifySelector(ctx, c, owner, selector.Kind, value)
		}
		if err != nil || !allowed {
			return ErrV5Denied
		}
	}
	seen := map[string]bool{}
	for name, ref := range cfg.SecretRefs {
		if part == "values" {
			continue
		}
		declared := false
		for _, n := range d.SecretNames {
			if n == name {
				declared = true
			}
		}
		if !declared || seen[ref] || !pluginV5SecretRefPattern.MatchString(ref) {
			return ErrV5Denied
		}
		seen[ref] = true
		allowed, err := s.configResourceAllowed(ctx, c, owner, "secret_ref", name, ref)
		if s.VerifySecretRef != nil {
			err = s.VerifySecretRef(ctx, c, owner, name, ref)
			allowed = err == nil
		}
		if err != nil || !allowed {
			return ErrV5Denied
		}
	}
	return nil
}
func (s *V5Platform) configuration(ctx context.Context, c V5Coordinate, owner string, m *PluginV5Manifest) (V5Configuration, error) {
	var cfg V5Configuration
	err := s.store.load(ctx, v5Config, v5VersionOwnerID(c, owner), &cfg)
	if err == nil {
		return cfg, nil
	}
	if err != ErrV5NotFound {
		return cfg, err
	}
	raw, _ := json.Marshal(m)
	d, err := parsePluginV5ConfigDefinition(raw)
	if err != nil {
		return cfg, err
	}
	return V5Configuration{V5Coordinate: c, Owner: owner, Values: append(json.RawMessage(nil), d.Defaults...), SecretRefs: map[string]string{}}, nil
}

func (s *V5Platform) validateInstanceConfiguration(ctx context.Context, c V5Coordinate, owner string, m *PluginV5Manifest) error {
	if m.Data.ConfigScope != "user" {
		cfg, err := s.configuration(ctx, c, "", m)
		if err != nil {
			return err
		}
		if err := s.validateConfig(ctx, c, "", m, cfg); err != nil {
			return err
		}
	}
	if owner != "" && m.Data.ConfigScope != "system" {
		cfg, err := s.configuration(ctx, c, owner, m)
		if err != nil {
			return err
		}
		return s.validateConfig(ctx, c, owner, m, cfg)
	}
	return nil
}
func v5MutationValid(r V5Mutation, admin bool) bool {
	if r.ExpiresAtMS < 0 || r.ExpiresAtMS != 0 && (!admin || r.Kind != "grant") {
		return false
	}
	if utf8.RuneCountInString(r.Reason) > 512 || !utf8.ValidString(r.Reason) || r.Reason != "" && (!admin || r.Kind != "grant" && r.Kind != "revoke") {
		return false
	}
	if r.ConfigurationPart != "" && (r.Kind != "configure" || r.ConfigurationPart != "values" && r.ConfigurationPart != "secret_refs" || r.ConfigurationPart == "values" && (r.Values == nil || r.SecretRefs != nil) || r.ConfigurationPart == "secret_refs" && (r.Values != nil || r.SecretRefs == nil)) {
		return false
	}
	if !v5Version(r.Version) || r.ExpectedGeneration < 0 || r.ExpectedRevision < 0 || !v5String(1, 128, `^[A-Za-z0-9_.:-]+$`)(r.IdempotencyKey) {
		return false
	}
	allowed := false
	if admin {
		switch r.Kind {
		case "install", "enable", "disable", "upgrade", "revoke", "uninstall", "grant", "configure", "purge_release", "retry":
			allowed = true
		}
	} else {
		switch r.Kind {
		case "install", "enable", "disable", "upgrade", "uninstall", "consent", "configure", "retry":
			allowed = true
		}
	}
	if !allowed {
		return false
	}
	if r.Kind != "grant" && (r.Approved || r.Consumes != nil || r.Resources != nil) {
		return false
	}
	if r.Kind != "consent" && (r.Accepted || r.PurposeHash != "") {
		return false
	}
	if r.Kind != "configure" && (r.Values != nil || r.SecretRefs != nil) {
		return false
	}
	if r.Kind != "uninstall" && r.Purge {
		return false
	}
	if r.Kind != "retry" && r.OperationID != "" {
		return false
	}
	return true
}

func (s *V5Platform) Import(ctx context.Context, raw, envelope []byte, key string, resolve identity.SessionPrincipalResolver) (V5Operation, error) {
	a, err := s.actor(ctx, true, "import", resolve, false)
	if err != nil {
		return V5Operation{}, err
	}
	if !v5String(1, 128, `^[A-Za-z0-9_.:-]+$`)(key) {
		return V5Operation{}, ErrV5Invalid
	}
	var result V5Operation
	err = s.catalog.WithVerified(ctx, raw, envelope, func(p *PluginV5Package) error {
		c := V5Coordinate{p.Manifest.Publisher, p.Manifest.Key, p.Manifest.Version}
		manifest, _ := json.Marshal(p.Manifest)
		request := V5Mutation{Kind: "import", Version: c.Version, IdempotencyKey: key}
		hash := v5BytesDigest(append(append([]byte(nil), raw...), envelope...))
		var e error
		result, e = s.command(ctx, a, c, "", request, hash, resolve, func(tx context.Context, op *V5Operation) error {
			if err := s.files.PutPackage(raw, p.Digest); err != nil {
				return err
			}
			r := V5PackageRelease{V5Coordinate: c, Digest: p.Digest, Size: p.Size, Manifest: manifest, Release: append(json.RawMessage(nil), envelope...), Storage: "available"}
			r.Revision, err = s.store.next(tx)
			if err != nil {
				return err
			}
			if err := s.store.save(tx, v5Release, c.id(), r); err != nil {
				return err
			}
			op.Status = "done"
			return nil
		})
		return e
	})
	return result, err
}
func (s *V5Platform) Mutate(ctx context.Context, publisher, key string, r V5Mutation, admin bool, resolve identity.SessionPrincipalResolver) (result V5Operation, err error) {
	c := V5Coordinate{publisher, key, r.Version}
	if !c.valid() || !v5MutationValid(r, admin) {
		return V5Operation{}, ErrV5Invalid
	}
	a, err := s.actor(ctx, admin, r.Kind, resolve, false)
	if err != nil {
		return V5Operation{}, err
	}
	owner := ""
	if !admin {
		owner = a.p.Actor.ID
	}
	raw, _ := json.Marshal(r)
	hash := v5BytesDigest(raw)
	changed := false
	defer func() {
		if err == nil && changed && result.Generation > 0 {
			if h := s.managed.Load(); h != nil {
				h.withdrawFamily(c.family(), owner)
			}
		}
	}()
	apply := func(m *PluginV5Manifest) error {
		var e error
		result, e = s.command(ctx, a, c, owner, r, hash, resolve, func(tx context.Context, op *V5Operation) error {
			changed = true
			return s.apply(tx, c, owner, r, m, op)
		})
		return e
	}
	needsVerification := r.Kind == "install" || r.Kind == "enable" || r.Kind == "upgrade" || r.Kind == "configure" || r.Kind == "grant" && r.Approved || r.Kind == "consent" && r.Accepted
	if !needsVerification {
		err := apply(nil)
		return result, err
	}
	release, err := s.release(ctx, c)
	if err != nil {
		return result, err
	}
	if release.Storage != "available" {
		return result, ErrV5PlatformUnavailable
	}
	archive, err := s.files.Package(release.Digest)
	if err != nil {
		return result, err
	}
	err = s.catalog.WithVerified(ctx, archive, release.Release, func(p *PluginV5Package) error {
		if p.Digest != release.Digest {
			return ErrPluginV5PackageDigest
		}
		return apply(p.Manifest)
	})
	return result, err
}

func (s *V5Platform) command(ctx context.Context, a v5Actor, c V5Coordinate, owner string, r V5Mutation, hash string, resolve identity.SessionPrincipalResolver, apply func(context.Context, *V5Operation) error) (V5Operation, error) {
	op := V5Operation{V5Coordinate: c, ID: v5RandomID(), ActorKind: a.p.Actor.Kind, ActorID: a.p.Actor.ID, Owner: owner, IdempotencyKey: r.IdempotencyKey, Reason: r.Reason, RequestHash: hash, Kind: r.Kind, Status: "queued", Purge: r.Purge}
	if op.ID == "" {
		return op, ErrV5PlatformUnavailable
	}
	err := s.reliable.Execute(ctx, reliability.Command{Code: "plugin.v5." + r.Kind, ActorType: a.p.Actor.Kind, ActorID: a.p.Actor.ID, ResourceType: "plugin_v5_operation", ResourceID: op.ID, PermissionCode: a.permission, OperationCode: "plugin.v5." + r.Kind, EventFactory: func() (reliability.Event, error) {
		return reliability.NewEvent("plugin.lifecycle.changed", "plugin_v5_operation", op.ID, map[string]any{"operation_id": op.ID, "publisher": c.Publisher, "plugin_key": c.Key, "version": c.Version, "generation": v5Counter(op.Generation), "status": op.Status})
	}}, func(tx context.Context) error {
		if err := s.recheck(tx, a, r.Kind, resolve); err != nil {
			return err
		}
		if err := s.store.lock(tx, "request:"+a.p.Actor.Kind+":"+a.p.Actor.ID+":"+r.IdempotencyKey); err != nil {
			return err
		}
		if err := s.store.lock(tx, c.family()); err != nil {
			return err
		}
		old, err := s.store.operation(tx, a.p.Actor.Kind, a.p.Actor.ID, r.IdempotencyKey)
		if err == nil {
			if old.RequestHash != hash || old.Kind != r.Kind || old.V5Coordinate != c || old.Owner != owner {
				return ErrV5Conflict
			}
			op = old
			if err := s.recheck(tx, a, r.Kind, resolve); err != nil {
				return err
			}
			return errV5Replay
		}
		if err != ErrV5NotFound {
			return err
		}
		if err := apply(tx, &op); err != nil {
			return err
		}
		if err := s.saveOp(tx, &op); err != nil {
			return err
		}
		return s.recheck(tx, a, r.Kind, resolve)
	})
	if err == errV5Replay {
		return op, nil
	}
	if err != nil {
		return V5Operation{}, err
	}
	return op, nil
}

func (s *V5Platform) apply(ctx context.Context, c V5Coordinate, owner string, r V5Mutation, m *PluginV5Manifest, op *V5Operation) error {
	release, err := s.release(ctx, c)
	if err != nil {
		return err
	}
	if m == nil {
		m, err = ParsePluginV5Manifest(release.Manifest)
		if err != nil {
			return err
		}
	}
	// The immutable metadata and actual file must still match after the family
	// lock; a concurrent purge/import cannot race this command into acceptance.
	if r.Kind == "install" || r.Kind == "enable" || r.Kind == "upgrade" || r.Kind == "configure" || r.Kind == "grant" && r.Approved || r.Kind == "consent" && r.Accepted {
		if release.Storage != "available" {
			return ErrV5PlatformUnavailable
		}
		if _, err := s.files.Package(release.Digest); err != nil {
			return err
		}
	}
	i, installErr := s.installation(ctx, c, owner)
	if installErr != nil && installErr != ErrV5NotFound {
		return installErr
	}
	if r.Kind == "grant" || r.Kind == "revoke" {
		if owner != "" {
			return ErrV5Denied
		}
		g, err := s.grant(ctx, c)
		if err != nil && err != ErrV5NotFound {
			return err
		}
		if g.Revision != r.ExpectedRevision {
			return ErrV5Conflict
		}
		g = V5Grant{V5Coordinate: c, Approved: r.Kind == "grant" && r.Approved, Consumes: r.Consumes, Resources: r.Resources, Reason: r.Reason, ApprovedBy: op.ActorID, ExpiresAtMS: r.ExpiresAtMS}
		if g.Consumes == nil {
			g.Consumes = []PluginV5Consume{}
		}
		if g.ExpiresAtMS != 0 && (g.ExpiresAtMS <= s.now().UnixMilli() || g.ExpiresAtMS > s.now().Add(30*24*time.Hour).UnixMilli()) {
			return ErrV5Invalid
		}
		if !v5GrantValid(m, g) {
			return ErrV5Denied
		}
		g.Revision, err = s.store.next(ctx)
		if err != nil {
			return err
		}
		if err = s.store.save(ctx, v5Grant, c.id(), g); err != nil {
			return err
		}
		if installErr == nil && (i.Version == c.Version || i.DesiredVersion == c.Version) {
			i.Desired = "disabled"
			if !i.Installed {
				i.Desired = "uninstalled"
			}
			return s.queueInstall(ctx, &i, op)
		}
		op.Status = "done"
		return nil
	}
	if r.Kind == "purge_release" {
		if owner != "" {
			return ErrV5Denied
		}
		return s.purgeRelease(ctx, c, release, op)
	}
	if r.Kind == "retry" {
		var old V5Operation
		if s.store.load(ctx, v5Operation, r.OperationID, &old) != nil {
			return ErrV5NotFound
		}
		if old.Owner != owner || old.V5Coordinate.family() != c.family() || old.Version != c.Version || !op.ActorKindIsAdmin() && old.ActorID != op.ActorID {
			return ErrV5Denied
		}
		if old.Revision != r.ExpectedRevision || old.Status != "failed" && old.Status != "waiting" {
			return ErrV5Conflict
		}
		old.Status = "queued"
		old.Error = ""
		old.LeaseUntil = 0
		old.Lease = 0
		if err := s.saveOp(ctx, &old); err != nil {
			return err
		}
		op.Status = "done"
		return nil
	}
	global, gerr := s.installation(ctx, c, "")
	needsGlobal := r.Kind == "install" || r.Kind == "enable" || r.Kind == "upgrade" || r.Kind == "configure" || r.Kind == "consent" && r.Accepted
	if owner != "" && needsGlobal && (gerr != nil || !global.Installed || global.Version != c.Version) {
		return ErrV5Denied
	}
	g, grantErr := s.grant(ctx, c)
	if r.Kind == "consent" {
		if owner == "" || r.PurposeHash != V5PurposeHash(m) || r.Accepted && (grantErr != nil || !v5GrantActive(g, s.now().UnixMilli())) {
			return ErrV5Denied
		}
		var old V5Consent
		err := s.store.load(ctx, v5Consent, v5VersionOwnerID(c, owner), &old)
		if err != nil && err != ErrV5NotFound {
			return err
		}
		if old.Revision != r.ExpectedRevision {
			return ErrV5Conflict
		}
		if !r.Accepted && old.Revision == 0 {
			return ErrV5NotFound
		}
		if !r.Accepted && g.Revision == 0 {
			g.Revision = old.GrantRevision
		}
		n, err := s.store.next(ctx)
		if err != nil {
			return err
		}
		consent := V5Consent{V5Coordinate: c, Owner: owner, Accepted: r.Accepted, PurposeHash: r.PurposeHash, GrantRevision: g.Revision, Revision: n}
		if err := s.store.save(ctx, v5Consent, v5VersionOwnerID(c, owner), consent); err != nil {
			return err
		}
		if installErr == nil && i.Installed {
			if !r.Accepted {
				i.Desired = "disabled"
			}
			return s.queueInstall(ctx, &i, op)
		}
		op.Status = "done"
		return nil
	}
	if r.Kind == "configure" {
		if installErr != nil || !i.Installed || owner == "" && m.Data.ConfigScope == "user" || owner != "" && m.Data.ConfigScope == "system" {
			return ErrV5Denied
		}
		cfg, err := s.configuration(ctx, c, owner, m)
		if err != nil {
			return err
		}
		if cfg.Revision != r.ExpectedRevision {
			return ErrV5Conflict
		}
		if r.ConfigurationPart != "secret_refs" {
			cfg.Values = r.Values
		}
		if r.ConfigurationPart != "values" {
			cfg.SecretRefs = r.SecretRefs
		}
		if cfg.SecretRefs == nil {
			cfg.SecretRefs = map[string]string{}
		}
		if err := s.validateConfigPart(ctx, c, owner, m, cfg, r.ConfigurationPart); err != nil {
			return err
		}
		cfg.Revision, err = s.store.next(ctx)
		if err != nil {
			return err
		}
		if err = s.store.save(ctx, v5Config, v5VersionOwnerID(c, owner), cfg); err != nil {
			return err
		}
		// Staging a known version's private configuration must not disturb the
		// running version or require an upgrade that already needs this config.
		if i.Version != c.Version && i.DesiredVersion != c.Version {
			op.Status = "done"
			return nil
		}
		return s.queueInstall(ctx, &i, op)
	}
	if installErr == ErrV5NotFound {
		if r.Kind != "install" || r.ExpectedGeneration != 0 {
			return ErrV5Conflict
		}
		i = V5Installation{Publisher: c.Publisher, Key: c.Key, Owner: owner, Version: c.Version, DesiredVersion: c.Version, Desired: "uninstalled", Observed: "stopped"}
	}
	if i.Generation != r.ExpectedGeneration {
		return ErrV5Conflict
	}
	switch r.Kind {
	case "install":
		if i.Installed {
			return ErrV5Conflict
		}
		if owner != "" && (grantErr != nil || !v5GrantActive(g, s.now().UnixMilli())) {
			return ErrV5Denied
		}
		if err := s.compatible(ctx, i, c, m); err != nil {
			return err
		}
		i.Installed = true
		i.Version = c.Version
		i.DesiredVersion = c.Version
		i.Desired = "disabled"
	case "enable":
		if !i.Installed || i.Version != c.Version || grantErr != nil || !v5GrantCompleteAt(m, g, s.now().UnixMilli()) || !s.consentOK(ctx, c, owner, m, g) {
			return ErrV5Denied
		}
		if err := s.validateInstanceConfiguration(ctx, c, owner, m); err != nil {
			return err
		}
		i.Desired = "enabled"
		i.DesiredVersion = c.Version
	case "disable":
		if c.Version != i.Version {
			return ErrV5Conflict
		}
		if !i.Installed {
			return ErrV5Conflict
		}
		i.Desired = "disabled"
	case "uninstall":
		if c.Version != i.Version {
			return ErrV5Conflict
		}
		if !i.Installed {
			return ErrV5Conflict
		}
		i.Installed = false
		i.Desired = "uninstalled"
	case "upgrade":
		if !i.Installed || i.Version == c.Version || grantErr != nil || !v5GrantCompleteAt(m, g, s.now().UnixMilli()) || !s.consentOK(ctx, c, owner, m, g) {
			return ErrV5Denied
		}
		if err := s.compatible(ctx, i, c, m); err != nil {
			return err
		}
		i.DesiredVersion = c.Version
	default:
		return ErrV5Invalid
	}
	return s.queueInstall(ctx, &i, op)
}
func (op V5Operation) ActorKindIsAdmin() bool { return op.ActorKind == "admin" }
func (s *V5Platform) queueInstall(ctx context.Context, i *V5Installation, op *V5Operation) error {
	n, err := s.store.next(ctx)
	if err != nil {
		return err
	}
	i.Generation = n
	i.Observed = "pending"
	i.Error = ""
	if i.Desired != "enabled" {
		i.ActiveGeneration = 0
	}
	op.Generation = n
	return s.saveInstall(ctx, i)
}
func (s *V5Platform) compatible(ctx context.Context, i V5Installation, c V5Coordinate, m *PluginV5Manifest) error {
	if i.Version == "" || i.Version == c.Version {
		return nil
	}
	old, err := s.release(ctx, i.coordinate())
	if err != nil {
		return err
	}
	previous, err := ParsePluginV5Manifest(old.Manifest)
	if err != nil {
		return err
	}
	if m.Data.Upgrade.Mode != "preserve" || previous.Data.Upgrade.SchemaVersion != m.Data.Upgrade.SchemaVersion {
		return ErrV5Rollback
	}
	// Per-version config is retained. Reusing values is explicit: no implicit
	// migration or unverified snapshot claim can overwrite the old version.
	return s.validateInstanceConfiguration(ctx, c, i.Owner, m)
}
func (s *V5Platform) purgeRelease(ctx context.Context, c V5Coordinate, r V5PackageRelease, op *V5Operation) error {
	found, err := s.store.referenced(ctx, c)
	if err != nil {
		return err
	}
	if found {
		return ErrV5Conflict
	}
	op.StorageRevision = r.Revision
	return nil
}
