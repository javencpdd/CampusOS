package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	identity "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestV5UIOnlyLifecycleActualPostgres(t *testing.T) {
	dsn := os.Getenv("CAMPUSOS_V5_LIFECYCLE_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires owned07 lifecycle PG")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil || cfg.ConnConfig.Database != "campusos_v12_07_upgrade" {
		t.Fatal("requires exact owned07 PG")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	h := NewV5UILifecycleTestHarness(t, ctx, pool)
	a := h.Coordinate
	host, err := NewV5UIHost(h.Platform, "http://{isolation}.plugin-ui.test", []string{"http://admin.test", "http://web.test"})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(host)
	defer server.Close()
	users := map[string]identity.SessionPrincipalResolver{"2801001": h.User, "2801002": h.UserSession(t, "2801002")}
	seq := 0
	detail := func(tt *testing.T, c V5Coordinate, owner string) V5InventoryEntry {
		tt.Helper()
		admin, proof := true, h.Admin
		if owner != "" {
			admin, proof = false, users[owner]
		}
		e, err := h.Platform.Detail(ctx, c, admin, proof)
		if err != nil {
			tt.Fatal(err)
		}
		return e
	}
	command := func(tt *testing.T, c V5Coordinate, owner string, r V5Mutation) V5Operation {
		tt.Helper()
		admin, proof := true, h.Admin
		if owner != "" {
			admin, proof = false, users[owner]
		}
		e := detail(tt, c, owner)
		seq++
		r.Version = c.Version
		r.IdempotencyKey = a.Key + "-ui-" + fmt.Sprint(seq)
		if e.Installation != nil {
			r.ExpectedGeneration = e.Installation.Generation
		}
		switch r.Kind {
		case "grant", "revoke":
			if e.Grant != nil {
				r.ExpectedRevision = e.Grant.Revision
			}
		case "consent":
			r.PurposeHash = e.PurposeHash
			if e.Consent != nil {
				r.ExpectedRevision = e.Consent.Revision
			}
		case "configure":
			if e.Configuration != nil {
				r.ExpectedRevision = e.Configuration.Revision
			}
		}
		op, err := h.Platform.Mutate(ctx, c.Publisher, c.Key, r, admin, proof)
		if err != nil {
			tt.Fatal(r.Kind, owner, err)
		}
		return op
	}
	approve := func(tt *testing.T, c V5Coordinate) {
		tt.Helper()
		m, err := ParsePluginV5Manifest(detail(tt, c, "").Release.Manifest)
		if err != nil {
			tt.Fatal(err)
		}
		command(tt, c, "", V5Mutation{Kind: "grant", Approved: true, Consumes: m.Consumes, Resources: m.HostResources})
	}
	configure := func(tt *testing.T, c V5Coordinate, owner, value string) {
		tt.Helper()
		raw, _ := json.Marshal(map[string]string{"message": value})
		command(tt, c, owner, V5Mutation{Kind: "configure", ConfigurationPart: "values", Values: raw})
	}
	drive := func(tt *testing.T, c V5Coordinate, owner string) {
		tt.Helper()
		until := time.Now().Add(10 * time.Second)
		for {
			if _, err := h.Platform.ProcessOnce(ctx, 50); err != nil {
				tt.Fatal(err)
			}
			i := h.Installation(tt, c, owner)
			if i.Version == c.Version && i.DesiredVersion == c.Version && i.Desired == "enabled" && i.Observed == "stopped" && i.ActiveGeneration == 0 {
				return
			}
			if time.Now().After(until) {
				tt.Fatal("UI asset activation did not converge without backend", i)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	deny := func(tt *testing.T, c V5Coordinate, owner string) {
		tt.Helper()
		admin, proof := true, h.Admin
		if owner != "" {
			admin, proof = false, users[owner]
		}
		i := h.Installation(tt, c, owner)
		seq++
		_, err := h.Platform.Mutate(ctx, c.Publisher, c.Key, V5Mutation{Kind: "upgrade", Version: c.Version, ExpectedGeneration: i.Generation, IdempotencyKey: a.Key + "-ui-deny-" + fmt.Sprint(seq)}, admin, proof)
		if err != ErrV5Denied {
			tt.Fatal("UI version inherited authority", err)
		}
	}
	write := func(tt *testing.T, c V5Coordinate, owner, value string) {
		tt.Helper()
		root := h.PrivateFiles(tt, c, owner)
		defer root.Close()
		if err := root.WriteFile("state", []byte(value), 0600); err != nil {
			tt.Fatal(err)
		}
	}
	read := func(tt *testing.T, c V5Coordinate, owner string) string {
		tt.Helper()
		root := h.PrivateFiles(tt, c, owner)
		defer root.Close()
		raw, err := root.ReadFile("state")
		if os.IsNotExist(err) {
			return ""
		}
		if err != nil {
			tt.Fatal(err)
		}
		return string(raw)
	}
	issue := func(tt *testing.T, c V5Coordinate, owner string) string {
		tt.Helper()
		admin, proof, parent := true, h.Admin, "http://admin.test"
		if owner != "" {
			admin, proof, parent = false, users[owner], "http://web.test"
		}
		_, src, err := host.issue(ctx, c, admin, parent, proof)
		if err != nil {
			tt.Fatal("current UI lease", err)
		}
		return src
	}
	fetch := func(tt *testing.T, src string, status int, marker string) {
		tt.Helper()
		u, err := url.Parse(src)
		if err != nil {
			tt.Fatal(err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+u.RequestURI(), nil)
		if err != nil {
			tt.Fatal(err)
		}
		req.Host = u.Host
		resp, err := server.Client().Do(req)
		if err != nil {
			tt.Fatal(err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
		if err != nil || resp.StatusCode != status {
			tt.Fatal("actual isolated UI response", resp.StatusCode, err)
		}
		if status == 200 && (!strings.Contains(string(raw), marker) || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "connect-src 'none'") || resp.Header.Get("Cache-Control") != "private, no-store") {
			tt.Fatal("UI bytes or isolation headers changed")
		}
	}
	var b V5Coordinate
	old := map[string]string{}
	t.Run("signed-assets-without-backend-readiness", func(t *testing.T) {
		configure(t, a, "", "ui-system-a")
		command(t, a, "", V5Mutation{Kind: "enable"})
		drive(t, a, "")
		write(t, a, "", "ui-system-private-a")
		old[""] = issue(t, a, "")
		fetch(t, old[""], 200, "Isolated UI")
		for _, owner := range []string{"2801001", "2801002"} {
			command(t, a, owner, V5Mutation{Kind: "install"})
			command(t, a, owner, V5Mutation{Kind: "consent", Accepted: true})
			configure(t, a, owner, "ui-"+owner+"-a")
			command(t, a, owner, V5Mutation{Kind: "enable"})
			drive(t, a, owner)
			write(t, a, owner, "ui-private-"+owner+"-a")
			old[owner] = issue(t, a, owner)
			fetch(t, old[owner], 200, "Isolated UI")
		}
		b = h.ImportUIVersion("8.2.0", "UI-version-b")
		if detail(t, b, "").Grant != nil {
			t.Fatal("UI candidate inherited grant")
		}
		deny(t, b, "")
		approve(t, b)
		configure(t, b, "", "ui-system-b")
	})
	if t.Failed() {
		return
	}
	current := map[string]string{}
	t.Run("version-cutover-fences-old-ui-leases", func(t *testing.T) {
		command(t, b, "", V5Mutation{Kind: "upgrade"})
		drive(t, b, "")
		for _, src := range old {
			fetch(t, src, 404, "")
		}
		current[""] = issue(t, b, "")
		fetch(t, current[""], 200, "UI-version-b")
		write(t, b, "", "ui-system-private-b")
		for _, owner := range []string{"2801001", "2801002"} {
			deny(t, b, owner)
			command(t, b, owner, V5Mutation{Kind: "consent", Accepted: true})
			configure(t, b, owner, "ui-"+owner+"-b")
			command(t, b, owner, V5Mutation{Kind: "upgrade"})
			drive(t, b, owner)
			current[owner] = issue(t, b, owner)
			fetch(t, current[owner], 200, "UI-version-b")
			write(t, b, owner, "ui-private-"+owner+"-b")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("ui-rollback-rechecks-current-authority-and-owner-snapshots", func(t *testing.T) {
		command(t, a, "", V5Mutation{Kind: "revoke"})
		deny(t, a, "")
		approve(t, a)
		command(t, a, "", V5Mutation{Kind: "upgrade"})
		drive(t, a, "")
		if read(t, a, "") != "ui-system-private-a" {
			t.Fatal("UI system checkpoint not restored")
		}
		for _, src := range current {
			fetch(t, src, 404, "")
		}
		current[""] = issue(t, a, "")
		fetch(t, current[""], 200, "Isolated UI")
		for _, owner := range []string{"2801001", "2801002"} {
			deny(t, a, owner)
			if read(t, b, owner) != "ui-private-"+owner+"-b" {
				t.Fatal("UI rollback affected other owner")
			}
			command(t, a, owner, V5Mutation{Kind: "consent", Accepted: true})
			command(t, a, owner, V5Mutation{Kind: "upgrade"})
			drive(t, a, owner)
			if read(t, a, owner) != "ui-private-"+owner+"-a" {
				t.Fatal("UI owner checkpoint mixed data")
			}
			var values map[string]string
			if err := json.Unmarshal(detail(t, a, owner).Configuration.Values, &values); err != nil || values["message"] != "ui-"+owner+"-a" {
				t.Fatal("UI owner config not restored", err)
			}
			current[owner] = issue(t, a, owner)
			fetch(t, current[owner], 200, "Isolated UI")
		}
	})
	if t.Failed() {
		return
	}
	t.Run("ui-owner-purge-and-unreferenced-package-cleanup", func(t *testing.T) {
		command(t, a, "2801001", V5Mutation{Kind: "uninstall", Purge: true})
		if _, err := h.Platform.ProcessOnce(ctx, 50); err != nil {
			t.Fatal(err)
		}
		fetch(t, current["2801001"], 404, "")
		fetch(t, current["2801002"], 200, "Isolated UI")
		fetch(t, current[""], 200, "Isolated UI")
		if read(t, a, "2801001") != "" || read(t, a, "2801002") != "ui-private-2801002-a" {
			t.Fatal("UI purge escaped owner")
		}
		command(t, a, "2801002", V5Mutation{Kind: "uninstall", Purge: true})
		command(t, a, "", V5Mutation{Kind: "uninstall", Purge: true})
		if _, err := h.Platform.ProcessOnce(ctx, 50); err != nil {
			t.Fatal(err)
		}
		fetch(t, current[""], 404, "")
		for _, c := range []V5Coordinate{a, b} {
			command(t, c, "", V5Mutation{Kind: "purge_release"})
			if _, err := h.Platform.ProcessOnce(ctx, 50); err != nil {
				t.Fatal(err)
			}
			if detail(t, c, "").Release.Storage != "purged" {
				t.Fatal("UI release cleanup not completed")
			}
		}
	})
}
