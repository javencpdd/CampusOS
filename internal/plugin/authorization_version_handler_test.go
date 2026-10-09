package plugin

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

type versionedHandlerFixture struct {
	service         *AuthorizationService
	store           AuthorizationStore
	userID          string
	pluginName      string
	retiredVersion  PluginVersion
	activeVersion   PluginVersion
	otherVersion    PluginVersion
	originalGrant   map[int64]int64
	originalConsent map[int64]int64
	delegationCount func(*testing.T) int
}

func TestAuthorizationVersionedHandlersRequireMatchingActiveVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Run("memory", func(t *testing.T) {
		runVersionedHandlerAssertions(t, newVersionedHandlerFixture(t, nil))
	})
	t.Run("postgres", func(t *testing.T) {
		dsn := os.Getenv("CAMPUSOS_PG_INTEGRATION_DSN")
		if dsn == "" {
			t.Skip("set CAMPUSOS_PG_INTEGRATION_DSN for PostgreSQL handler assertions")
		}
		pool, err := pgxpool.New(context.Background(), dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		runVersionedHandlerAssertions(t, newVersionedHandlerFixture(t, pool))
	})
}

func newVersionedHandlerFixture(t *testing.T, pool *pgxpool.Pool) versionedHandlerFixture {
	t.Helper()
	ctx := context.Background()
	userID := idgen.New()
	userIDText := strconv.FormatInt(userID, 10)
	firstID, secondID := idgen.New(), idgen.New()
	firstName := fmt.Sprintf("v12-route-%d-a", firstID)
	secondName := fmt.Sprintf("v12-route-%d-b", secondID)

	var repo PluginRepository
	var store AuthorizationStore
	if pool == nil {
		repo = NewMemoryPluginRepository()
		store = NewMemoryAuthorizationStore()
	} else {
		if _, err := pool.Exec(ctx, "INSERT INTO users(id,username,nickname,email) VALUES($1,$2,$3,$4)",
			userID, "v12_route_"+userIDText, "Versioned Route Test", "v12-route-"+userIDText+"@example.invalid"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), "DELETE FROM plugin_authorization_decisions WHERE plugin_version_id IN (SELECT id FROM plugin_versions WHERE plugin_id IN ($1,$2))", firstID, secondID)
			_, _ = pool.Exec(context.Background(), "DELETE FROM plugins WHERE id IN ($1,$2)", firstID, secondID)
			_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", userID)
		})
		repo = NewPgPluginRepository(pool)
		store = NewPgAuthorizationStore(pool)
	}

	now := time.Now()
	for _, entry := range []struct {
		id   int64
		name string
	}{
		{firstID, firstName},
		{secondID, secondName},
	} {
		if err := repo.Save(ctx, &PluginRecord{
			ID: entry.id, Name: entry.name, DisplayName: entry.name,
			Version: "1.0.0", Runtime: "process", Status: "running",
			BackendState: "running", FrontendState: "unloaded", HealthState: "healthy",
			Config: "{}", Checksum: strings.Repeat("a", 64),
			InstalledBy: "test", InstalledAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	service := NewAuthorizationService(store, repo, func(string) bool { return true })
	syncVersion := func(name, version, digest string) PluginVersion {
		t.Helper()
		manifest := versionTestManifest(version)
		manifest.Name, manifest.DisplayName = name, name
		result, err := service.SyncInstalled(ctx, &Plugin{Manifest: manifest, Checksum: strings.Repeat(digest, 64)}, userIDText)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	seedAuthorization := func(version PluginVersion) (int64, int64) {
		t.Helper()
		grant, err := service.SetAdminGrant(ctx, version.ID, "schedule.self.read", "granted", "handler test",
			map[string]interface{}{"scope": "self"}, userIDText, nil)
		if err != nil {
			t.Fatal(err)
		}
		consent, err := service.SetUserConsent(ctx, userIDText, version.ID, "schedule.self.read", "granted",
			map[string]interface{}{"scope": "self"})
		if err != nil {
			t.Fatal(err)
		}
		return grant.ID, consent.ID
	}
	retired := syncVersion(firstName, "1.0.0", "a")
	retiredGrant, retiredConsent := seedAuthorization(retired)
	other := syncVersion(secondName, "1.0.0", "b")
	otherGrant, otherConsent := seedAuthorization(other)
	active := syncVersion(firstName, "1.1.0", "c")
	fixture := versionedHandlerFixture{
		service: service, store: store, userID: userIDText, pluginName: firstName,
		retiredVersion: retired, activeVersion: active, otherVersion: other,
		originalGrant:   map[int64]int64{retired.ID: retiredGrant, other.ID: otherGrant},
		originalConsent: map[int64]int64{retired.ID: retiredConsent, other.ID: otherConsent},
	}
	if pool == nil {
		memory := store.(*MemoryAuthorizationStore)
		fixture.delegationCount = func(*testing.T) int {
			memory.mu.RLock()
			defer memory.mu.RUnlock()
			return len(memory.delegations)
		}
	} else {
		fixture.delegationCount = func(t *testing.T) int {
			t.Helper()
			var count int
			if err := pool.QueryRow(ctx,
				"SELECT count(*) FROM plugin_delegations WHERE subject_user_id=$1 AND plugin_version_id IN ($2,$3,$4)",
				userID, retired.ID, active.ID, other.ID).Scan(&count); err != nil {
				t.Fatal(err)
			}
			return count
		}
	}
	return fixture
}

func runVersionedHandlerAssertions(t *testing.T, fixture versionedHandlerFixture) {
	t.Helper()
	ctx := context.Background()
	handler := NewHandler(nil, WithAuthorizationService(fixture.service))
	router := gin.New()
	// Production authentication is installed by httpapi/router.go. This unit
	// test injects only the already authenticated user claim seen by handlers.
	router.Use(func(c *gin.Context) {
		c.Set("user_id", fixture.userID)
		c.Next()
	})
	router.PUT("/plugins/:name/versions/:version_id/grants/:capability", handler.AdminSetCapabilityGrant)
	router.PUT("/plugin-authorizations/:name/versions/:version_id/consents/:capability", handler.SetMyCapabilityConsent)
	router.POST("/plugin-authorizations/:name/versions/:version_id/delegations", handler.IssueMyDelegation)

	request := func(method, path, payload string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		httpRequest := httptest.NewRequest(method, path, strings.NewReader(payload))
		httpRequest.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(recorder, httpRequest)
		return recorder
	}
	grantPath := func(name string, versionID int64) string {
		return fmt.Sprintf("/plugins/%s/versions/%d/grants/schedule.self.read", name, versionID)
	}
	consentPath := func(name string, versionID int64) string {
		return fmt.Sprintf("/plugin-authorizations/%s/versions/%d/consents/schedule.self.read", name, versionID)
	}
	delegationPath := func(name string, versionID int64) string {
		return fmt.Sprintf("/plugin-authorizations/%s/versions/%d/delegations", name, versionID)
	}
	const revokeGrant = "{\"status\":\"revoked\",\"reason\":\"must not mutate another version\",\"scope\":{\"scope\":\"self\"}}"
	const revokeConsent = "{\"status\":\"revoked\",\"scope\":{\"scope\":\"self\"}}"
	const issueDelegation = "{\"capabilities\":[\"schedule.self.read\"],\"scope\":{\"scope\":\"self\"},\"ttl_seconds\":60}"

	for _, candidate := range []struct {
		name      string
		versionID int64
		label     string
	}{
		{fixture.pluginName, fixture.retiredVersion.ID, "retired version"},
		{fixture.pluginName, fixture.otherVersion.ID, "wrong plugin name"},
	} {
		before := fixture.delegationCount(t)
		for _, operation := range []struct {
			method  string
			path    string
			payload string
		}{
			{http.MethodPut, grantPath(candidate.name, candidate.versionID), revokeGrant},
			{http.MethodPut, consentPath(candidate.name, candidate.versionID), revokeConsent},
			{http.MethodPost, delegationPath(candidate.name, candidate.versionID), issueDelegation},
		} {
			result := request(operation.method, operation.path, operation.payload)
			if result.Code != http.StatusBadRequest || !strings.Contains(result.Body.String(), "插件版本已变化") {
				t.Fatalf("%s %s %s: status=%d body=%s", candidate.label, operation.method, operation.path, result.Code, result.Body.String())
			}
		}
		if after := fixture.delegationCount(t); after != before {
			t.Fatalf("%s created delegation: before=%d after=%d", candidate.label, before, after)
		}
		grant, err := fixture.store.CurrentAdminGrant(ctx, candidate.versionID, "schedule.self.read")
		if err != nil || grant.ID != fixture.originalGrant[candidate.versionID] || grant.Status != "granted" {
			t.Fatalf("%s changed grant: %+v err=%v", candidate.label, grant, err)
		}
		userID, _ := strconv.ParseInt(fixture.userID, 10, 64)
		consent, err := fixture.store.CurrentUserConsent(ctx, userID, candidate.versionID, "schedule.self.read")
		if err != nil || consent.ID != fixture.originalConsent[candidate.versionID] || consent.Status != "granted" {
			t.Fatalf("%s changed consent: %+v err=%v", candidate.label, consent, err)
		}
	}

	for _, operation := range []struct {
		method  string
		path    string
		payload string
	}{
		{http.MethodPut, grantPath(fixture.pluginName, fixture.activeVersion.ID),
			"{\"status\":\"granted\",\"reason\":\"active version\",\"scope\":{\"scope\":\"self\"}}"},
		{http.MethodPut, consentPath(fixture.pluginName, fixture.activeVersion.ID),
			"{\"status\":\"granted\",\"scope\":{\"scope\":\"self\"}}"},
		{http.MethodPost, delegationPath(fixture.pluginName, fixture.activeVersion.ID), issueDelegation},
	} {
		result := request(operation.method, operation.path, operation.payload)
		if result.Code != http.StatusOK {
			t.Fatalf("active version %s %s: status=%d body=%s", operation.method, operation.path, result.Code, result.Body.String())
		}
	}
	if count := fixture.delegationCount(t); count != 1 {
		t.Fatalf("active version delegation count=%d, want 1", count)
	}
}
