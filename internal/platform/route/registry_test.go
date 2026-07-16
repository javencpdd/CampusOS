package route

import "testing"

func TestRegistryRejectsDuplicateMethodAndPath(t *testing.T) {
	registry := NewRegistry()
	first := Descriptor{ID: "identity.health", OperationCode: "http.identity.health", Owner: "core.identity", Method: "GET", Path: "/api/v1/health", Audience: AudiencePublic, Auth: "none"}
	if err := registry.Add(first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID = "other.health"
	if err := registry.Add(second); err == nil {
		t.Fatal("expected route conflict")
	}
}

func TestDescriptorRequiresAdminPermission(t *testing.T) {
	descriptor := Descriptor{ID: "admin.users", OperationCode: "http.identity.users", Owner: "core.identity", Method: "GET", Path: "/api/v1/users", Audience: AudienceAdmin, Auth: "jwt"}
	if err := descriptor.Validate(); err == nil {
		t.Fatal("expected missing permission metadata error")
	}
}

func TestRegistryOrdersDescriptorsDeterministically(t *testing.T) {
	registry := NewRegistry()
	for _, descriptor := range []Descriptor{
		{ID: "community.post", OperationCode: "http.community.post", Owner: "core.community", Method: "POST", Path: "/api/v1/threads", Audience: AudienceAuthenticated, Auth: "jwt"},
		{ID: "community.list", OperationCode: "http.community.list", Owner: "core.community", Method: "GET", Path: "/api/v1/threads", Audience: AudiencePublic, Auth: "none"},
	} {
		if err := registry.Add(descriptor); err != nil {
			t.Fatal(err)
		}
	}
	items := registry.Descriptors()
	if len(items) != 2 || items[0].ID != "community.list" || items[1].ID != "community.post" {
		t.Fatalf("unexpected descriptors: %#v", items)
	}
}
