package runtimeconfig

import (
	"context"
	"time"

	identityport "github.com/campusos/CampusOS/internal/modules/core/identity/port"
	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/pkg/config"
)

type DocumentConfirmation struct {
	Revision           string `json:"revision"`
	VerificationMethod string `json:"verification_method"`
	CurrentPassword    string `json:"current_password,omitempty"`
}
type Document struct {
	Content  string `json:"content"`
	Revision string `json:"revision"`
}

// Full document disclosure requires manage authority and fresh confirmation.
// Credentials and file content never enter audit/outbox or reusable grants.
func (s *Service) ReadDocument(ctx context.Context, resolve identityport.SessionPrincipalResolver, input DocumentConfirmation) (Document, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if input.VerificationMethod != "password" && input.VerificationMethod != "mfa" || input.VerificationMethod == "password" && (len(input.CurrentPassword) == 0 || len(input.CurrentPassword) > 72) || input.VerificationMethod != "password" && input.CurrentPassword != "" {
		return Document{}, ErrVerificationInvalid
	}
	confirmation := Update{VerificationMethod: input.VerificationMethod, CurrentPassword: input.CurrentPassword}
	a, err := s.changeAuthority(ctx, resolve, confirmation, false)
	if err != nil {
		return Document{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg.Path == "" || s.cfg.Error != nil {
		return Document{}, ErrUnavailable
	}
	var out Document
	check := func(tx context.Context) error {
		current, e := s.changeAuthority(tx, resolve, confirmation, true)
		if e != nil {
			return e
		}
		return identityport.CheckAdminExecutionAuthority(a, current)
	}
	event, err := reliability.NewEvent("platform.runtime_config.document.read", "runtime_config", "current", map[string]any{"verification_method": input.VerificationMethod})
	if err != nil {
		return out, ErrUnavailable
	}
	err = s.reliable.Execute(ctx, reliability.Command{Code: "platform.runtime_config.document.read", ActorType: "admin", ActorID: a.Principal.Actor.ID, ResourceType: "runtime_config", ResourceID: "current", OperationCode: "http.platform.runtime_config.document", PermissionCode: "platform.runtime_config.manage", Event: &event}, check)
	if err != nil {
		return out, ErrUnavailable
	}
	err = s.reliable.CheckedRead(ctx, func(tx context.Context) error {
		if e := check(tx); e != nil {
			return e
		}
		doc, e := config.ReadRuntimeDocument(s.cfg.Path)
		if e != nil {
			return ErrUnavailable
		}
		if doc.Revision != input.Revision {
			return config.ErrManagedConflict
		}
		out = Document{Content: string(doc.Raw), Revision: doc.Revision}
		return nil
	})
	if err != nil {
		return Document{}, err
	}
	return out, nil
}
