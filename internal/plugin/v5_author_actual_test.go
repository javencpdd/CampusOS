package plugin_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/plugin"
	container "github.com/campusos/CampusOS/internal/plugin/container"
	sdk "github.com/campusos/CampusOS/sdk/go"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestV5AuthorContainerActualPostgres(t *testing.T) {
	dsn, artifacts := os.Getenv("CAMPUSOS_V5_AUTHOR_DATABASE_URL"), os.Getenv("CAMPUSOS_V5_AUTHOR_ARTIFACTS")
	if dsn == "" || artifacts == "" {
		t.Skip("requires owned08a PostgreSQL and independently built signed author artifacts")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != "campusos_v12_08a_author" || !filepath.IsAbs(artifacts) || !strings.Contains(filepath.ToSlash(artifacts), "/.cache/v12-08a-author/acceptance.") {
		t.Fatal("requires exact owned08a target resources")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	read := func(name string) []byte {
		t.Helper()
		raw, err := os.ReadFile(filepath.Join(artifacts, name))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	archive, envelope, trust := read("echo.tar.gz"), read("echo.release.json"), read("trust.json")
	p, err := plugin.VerifyPluginV5Release(archive, envelope, trust)
	if err != nil || p.Manifest.Runtime != "container" || p.Manifest.Backend.Transport != "http" || len(p.Manifest.Consumes) != 0 || len(p.Manifest.UI) != 0 || !strings.HasPrefix(p.Manifest.Backend.Artifact.Image, "localhost/campusos-owned-v12-08a-author@sha256:") {
		t.Fatal("requires actual signed headless author package", err)
	}
	// Fingerprint the executable and native process identity, not a mutable tag.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	binaryHash := sha256.Sum256(binary)
	start, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		t.Fatal(err)
	}
	identityFields := strings.Fields(string(start[strings.LastIndexByte(string(start), ')')+2:]))
	startTime := identityFields[19]
	fingerprint := func() string {
		var value string
		err := pool.QueryRow(ctx, `SELECT md5(COALESCE(string_agg(row_to_json(c)::text,E'\n' ORDER BY table_name,ordinal_position),'')) FROM (SELECT table_name,column_name,ordinal_position,data_type,is_nullable,column_default FROM information_schema.columns WHERE table_schema='public') c`).Scan(&value)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	schemaBefore := fingerprint()
	h := plugin.NewV5AuthorTestHarness(t, ctx, pool, archive, envelope, trust)
	c := h.Coordinate
	factory, err := container.NewV5Factory(strings.Repeat("8", 64))
	if err != nil {
		t.Fatal(err)
	}
	defer factory.Close(context.Background())
	leader, err := plugin.NewPgV5StateStore(pool).AcquireRuntimeLeader(ctx)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := plugin.NewV5RuntimeCoordinator(h.Platform, h.Hub, factory, leader)
	defer coordinator.Close(context.Background())
	users := map[string]identity.SessionPrincipalResolver{"2801001": h.User, "2801002": h.UserSession(t, "2801002")}
	sequence := 0
	actor := func(owner string) (bool, identity.SessionPrincipalResolver) {
		if owner == "" {
			return true, h.Admin
		}
		return false, users[owner]
	}
	command := func(tt *testing.T, owner string, request plugin.V5Mutation) plugin.V5Operation {
		tt.Helper()
		admin, proof := actor(owner)
		d, err := h.Platform.Detail(ctx, c, admin, proof)
		if err != nil {
			tt.Fatal(err)
		}
		sequence++
		request.IdempotencyKey = fmt.Sprintf("author08a-%d", sequence)
		request.Version = c.Version
		if d.Installation != nil {
			request.ExpectedGeneration = d.Installation.Generation
		}
		if request.Kind == "grant" || request.Kind == "revoke" {
			if d.Grant != nil {
				request.ExpectedRevision = d.Grant.Revision
			}
		}
		if request.Kind == "configure" && d.Configuration != nil {
			request.ExpectedRevision = d.Configuration.Revision
		}
		op, err := h.Platform.Mutate(ctx, c.Publisher, c.Key, request, admin, proof)
		if err != nil {
			tt.Fatal(request.Kind, owner, err)
		}
		return op
	}
	configure := func(tt *testing.T, owner, prefix string) {
		tt.Helper()
		raw, _ := json.Marshal(map[string]string{"prefix": prefix})
		command(tt, owner, plugin.V5Mutation{Kind: "configure", ConfigurationPart: "values", Values: raw})
	}
	drive := func(tt *testing.T, owner, status string) {
		tt.Helper()
		until := time.Now().Add(15 * time.Second)
		for {
			if _, err := h.Platform.ProcessOnce(ctx, 50); err != nil {
				tt.Fatal(err)
			}
			if _, err := coordinator.ProcessOnce(ctx); err != nil {
				tt.Fatal(err)
			}
			i := h.Current(owner)
			if i.Observed == status {
				return
			}
			if time.Now().After(until) {
				tt.Fatal("real author runtime did not converge", i)
			}
			time.Sleep(40 * time.Millisecond)
		}
	}
	selectBinding := func(tt *testing.T, owner string) plugin.V5ManagedBinding {
		tt.Helper()
		b, err := h.Hub.Select(ctx, owner, plugin.V5ContributionQuery{Point: plugin.V5AIChat, ContractVersion: "v1", Capability: "text"}, &plugin.V5ContributionRef{V5Coordinate: c, ID: "chat-main"})
		if err != nil {
			tt.Fatal(err)
		}
		return b
	}
	invoke := func(callCtx context.Context, b plugin.V5ManagedBinding, proof identity.SessionPrincipalResolver, prompt, contextClass string, deadline int64) (sdk.V5ChatResponse, error) {
		request := sdk.V5ChatRequest{RequestID: "author08a", ContributionID: "chat-main", InstanceGeneration: b.Descriptor().InstanceGeneration, DeadlineMS: deadline, Prompt: prompt, ContextClass: contextClass}
		raw, _ := json.Marshal(request)
		wire, _ := json.Marshal(sdk.V5DispatchCall{RequestID: request.RequestID, Contract: "campusos.plugin-dispatch/v1", Target: sdk.V5DispatchTarget{Kind: "contribution", ID: request.ContributionID}, InstanceGeneration: request.InstanceGeneration, DeadlineMS: time.Now().Add(4 * time.Second).UnixMilli(), RequestBytes: int64(len(raw)), ResponseLimitBytes: 65536})
		out, err := h.Hub.Invoke(callCtx, h.Consumer, b, wire, raw, proof)
		if err != nil && len(out) != 0 {
			t.Errorf("failed typed call released %d raw bytes", len(out))
		}
		var response sdk.V5ChatResponse
		if err == nil {
			err = json.Unmarshal(out, &response)
		}
		return response, err
	}
	check := func(tt *testing.T, owner, prefix string) {
		tt.Helper()
		b := selectBinding(tt, owner)
		out, err := invoke(ctx, b, users[owner], "hello \"世界\"\n", "none", 3000)
		if err != nil || out.Text != prefix+"hello \"世界\"\n" || out.InstanceGeneration != b.Descriptor().InstanceGeneration || out.RequestID != "author08a" {
			tt.Fatal("actual authored guest response", out, err)
		}
	}
	var first, second plugin.V5ManagedBinding
	t.Run("signed-import-and-independent-current-approval", func(t *testing.T) {
		command(t, "", plugin.V5Mutation{Kind: "install"})
		_, err := h.Platform.Mutate(ctx, c.Publisher, c.Key, plugin.V5Mutation{Kind: "enable", Version: c.Version, ExpectedGeneration: h.Current("").Generation, IdempotencyKey: "author08a-without-grant"}, true, h.Admin)
		if err != plugin.ErrV5Denied || len(factory.Workers()) != 0 {
			t.Fatal("import/installation executed without approval", err)
		}
		command(t, "", plugin.V5Mutation{Kind: "grant", Approved: true, Consumes: p.Manifest.Consumes, Resources: p.Manifest.HostResources})
		configure(t, "", "system: ")
		command(t, "", plugin.V5Mutation{Kind: "enable"})
		drive(t, "", "ready")
	})
	if t.Failed() {
		return
	}
	t.Run("two-owner-real-typed-chat-and-configuration-isolation", func(t *testing.T) {
		for _, owner := range []string{"2801001", "2801002"} {
			command(t, owner, plugin.V5Mutation{Kind: "install"})
			configure(t, owner, owner+": ")
			command(t, owner, plugin.V5Mutation{Kind: "enable"})
			drive(t, owner, "ready")
			check(t, owner, owner+": ")
		}
		first, second = selectBinding(t, "2801001"), selectBinding(t, "2801002")
		if first.Descriptor().InstanceGeneration == second.Descriptor().InstanceGeneration || len(factory.Workers()) != 3 {
			t.Fatal("owner workers not independent")
		}
		if _, err := invoke(ctx, first, users["2801002"], "cross-owner", "none", 3000); err == nil {
			t.Fatal("cross owner binding released output")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("relative-deadline-and-current-configuration-fences", func(t *testing.T) {
		for _, test := range []struct {
			class    string
			deadline int64
		}{{"public", 3000}, {"none", 0}, {"none", 300001}} {
			if _, err := invoke(ctx, first, users["2801001"], "bad", test.class, test.deadline); err == nil {
				t.Fatal("unsupported request accepted", test)
			}
		}
		expired, stop := context.WithDeadline(ctx, time.Now().Add(-time.Second))
		defer stop()
		if _, err := invoke(expired, first, users["2801001"], "expired", "none", 3000); err == nil || !errors.Is(expired.Err(), context.DeadlineExceeded) {
			t.Fatal("expired request released output", err)
		}
		// Negative input checks must not supply the stale-binding oracle by
		// killing a worker before the configuration mutation is exercised.
		check(t, "2801001", "2801001: ")
		configure(t, "2801001", "changed: ")
		if _, err := invoke(ctx, first, users["2801001"], "stale", "none", 3000); err == nil {
			t.Fatal("old configuration binding survived")
		}
		check(t, "2801002", "2801002: ")
		drive(t, "2801001", "ready")
		check(t, "2801001", "changed: ")
		first = selectBinding(t, "2801001")
	})
	if t.Failed() {
		return
	}
	t.Run("reload-revocation-and-owner-uninstall", func(t *testing.T) {
		command(t, "2801001", plugin.V5Mutation{Kind: "disable"})
		drive(t, "2801001", "stopped")
		if _, err := invoke(ctx, first, users["2801001"], "stopped", "none", 3000); err == nil {
			t.Fatal("disabled binding survived")
		}
		check(t, "2801002", "2801002: ")
		command(t, "2801001", plugin.V5Mutation{Kind: "enable"})
		drive(t, "2801001", "ready")
		check(t, "2801001", "changed: ")
		command(t, "", plugin.V5Mutation{Kind: "revoke"})
		if _, err := invoke(ctx, second, users["2801002"], "revoked", "none", 3000); err == nil {
			t.Fatal("revoked grant released output")
		}
		uninstall := map[string]plugin.V5Operation{}
		for _, owner := range []string{"2801001", "2801002", ""} {
			uninstall[owner] = command(t, owner, plugin.V5Mutation{Kind: "uninstall", Purge: true})
		}
		for n := 0; n < 4; n++ {
			if _, err := h.Platform.ProcessOnce(ctx, 50); err != nil {
				t.Fatal(err)
			}
			if _, err := coordinator.ProcessOnce(ctx); err != nil {
				t.Fatal(err)
			}
		}
		for owner, op := range uninstall {
			view, err := h.Platform.Operation(ctx, op.ID, true, h.Admin)
			i := h.Current(owner)
			if err != nil || view.Status != "done" || i.Installed || i.Desired != "uninstalled" || i.Observed != "stopped" || i.ActiveGeneration != 0 {
				t.Fatal("owner uninstall did not complete", view, i, err)
			}
		}
		purge := command(t, "", plugin.V5Mutation{Kind: "purge_release"})
		if _, err := h.Platform.ProcessOnce(ctx, 50); err != nil {
			t.Fatal(err)
		}
		view, err := h.Platform.Operation(ctx, purge.ID, true, h.Admin)
		release, detailErr := h.Platform.Detail(ctx, c, true, h.Admin)
		_, fileErr := os.Stat(filepath.Join(h.ManagedRoot, "v5/packages", strings.TrimPrefix(p.Digest, "sha256:")+".tar.gz"))
		if err != nil || view.Status != "done" || detailErr != nil || release.Release.Storage != "purged" || !os.IsNotExist(fileErr) {
			t.Fatal("actual release purge did not complete", view, err, detailErr, fileErr)
		}
		if err := coordinator.Close(ctx); err != nil {
			t.Fatal(err)
		}
		if len(factory.Workers()) != 0 {
			t.Fatal("author workers leaked")
		}
	})
	if t.Failed() {
		return
	}
	if schemaBefore != fingerprint() {
		t.Fatal("author lifecycle modified host schema")
	}
	now, err := os.ReadFile(executable)
	if err != nil || sha256.Sum256(now) != binaryHash {
		t.Fatal("host executable changed", err)
	}
	stat, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		t.Fatal(err)
	}
	nowFields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+2:]))
	if nowFields[19] != startTime {
		t.Fatal("host process restarted")
	}
	proof, _ := json.MarshalIndent(map[string]any{"target": "native test host, not production API/SPA read-only deployment", "pid": os.Getpid(), "starttime": startTime, "binary_sha256": hex.EncodeToString(binaryHash[:]), "schema_columns_before": schemaBefore, "schema_columns_after": fingerprint(), "actual_signed_archive": p.Digest, "workers_after": len(factory.Workers()), "package": c}, "", "  ")
	if err := os.WriteFile(filepath.Join(artifacts, "native-host-proof.json"), append(proof, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
