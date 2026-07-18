// Package route defines static HTTP route ownership before modules register
// their Gin handlers. It is transport metadata, not a dynamic router.
package route

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

var operationCodePattern = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+){2,}$`)

type Audience string

const (
	AudiencePublic        Audience = "public"
	AudienceAuthenticated Audience = "authenticated"
	AudienceAdmin         Audience = "admin"
)

type Descriptor struct {
	ID             string   `json:"id"`
	OperationCode  string   `json:"operation_code,omitempty"`
	LegacyAliases  []string `json:"legacy_aliases,omitempty"`
	Owner          string   `json:"owner"`
	Method         string   `json:"method"`
	Path           string   `json:"path"`
	Audience       Audience `json:"audience"`
	Auth           string   `json:"auth"`
	Permission     string   `json:"permission,omitempty"`
	PermissionCode string   `json:"permission_code,omitempty"`
	FeatureID      string   `json:"feature_id,omitempty"`
	Audit          string   `json:"audit,omitempty"`
}

func (d Descriptor) Validate() error {
	if strings.TrimSpace(d.ID) == "" || strings.TrimSpace(d.Owner) == "" {
		return errors.New("route ID and owner are required")
	}
	method := strings.ToUpper(strings.TrimSpace(d.Method))
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
	default:
		return fmt.Errorf("route %q has unsupported method %q", d.ID, d.Method)
	}
	if !strings.HasPrefix(d.Path, "/") {
		return fmt.Errorf("route %q path must start with /", d.ID)
	}
	switch d.Audience {
	case AudiencePublic, AudienceAuthenticated, AudienceAdmin:
	default:
		return fmt.Errorf("route %q has unsupported audience %q", d.ID, d.Audience)
	}
	if d.Audience == AudienceAdmin && strings.TrimSpace(d.Permission) == "" {
		return fmt.Errorf("admin route %q requires permission metadata", d.ID)
	}
	if d.Audience == AudienceAdmin && strings.TrimSpace(d.PermissionCode) == "" {
		return fmt.Errorf("admin route %q requires a permission code", d.ID)
	}
	if strings.TrimSpace(d.OperationCode) == "" {
		return fmt.Errorf("route %q requires an operation code", d.ID)
	}
	if !operationCodePattern.MatchString(strings.TrimSpace(d.OperationCode)) {
		return fmt.Errorf("route %q has invalid operation code %q", d.ID, d.OperationCode)
	}
	return nil
}

type Groups struct {
	Public        *gin.RouterGroup
	Authenticated *gin.RouterGroup
	Admin         *gin.RouterGroup
}

// Contributor owns descriptors and static registration for one Module.
type Contributor interface {
	RouteDescriptors() []Descriptor
	RegisterRoutes(Groups) error
}

type Registry struct {
	mu       sync.RWMutex
	byID     map[string]Descriptor
	byMethod map[string]string
}

func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]Descriptor), byMethod: make(map[string]string)}
}

func (r *Registry) Add(descriptor Descriptor) error {
	if err := descriptor.Validate(); err != nil {
		return err
	}
	descriptor.Method = strings.ToUpper(strings.TrimSpace(descriptor.Method))
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.byID[descriptor.ID]; exists {
		return fmt.Errorf("route ID %q is already registered", descriptor.ID)
	}
	key := descriptor.Method + " " + descriptor.Path
	if existing, exists := r.byMethod[key]; exists {
		return fmt.Errorf("route %s conflicts with %q", key, existing)
	}
	r.byID[descriptor.ID] = descriptor
	r.byMethod[key] = descriptor.ID
	return nil
}

func (r *Registry) AddContributor(contributor Contributor) error {
	if contributor == nil {
		return errors.New("route contributor is required")
	}
	for _, descriptor := range contributor.RouteDescriptors() {
		if err := r.Add(descriptor); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) Descriptors() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]Descriptor, 0, len(r.byID))
	for _, descriptor := range r.byID {
		items = append(items, descriptor)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Method == items[j].Method {
			return items[i].Path < items[j].Path
		}
		return items[i].Method < items[j].Method
	})
	return items
}
