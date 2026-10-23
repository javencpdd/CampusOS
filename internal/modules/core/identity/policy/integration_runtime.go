package policy

import (
	"reflect"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

// Runtime v2 adds exactly the existing Community public-thread read pair.
// v1 remains unchanged. The compiled host inventory determines which concrete
// Knowledge/Community endpoints are currently available to machine callers.
type IntegrationRuntimePolicy struct{}

func runtimeShape(r port.IntegrationAccessInput) error {
	if r.Contract != port.IntegrationRuntimeContract {
		return port.ErrIntegrationInputInvalid
	}
	r.Contract = port.IntegrationAccessContract
	return r.Validate()
}
func (IntegrationRuntimePolicy) DecideIntegrationAccess(r port.IntegrationAccessInput) (port.IntegrationAccessDecision, error) {
	return decideIntegrationAccess(r, true)
}
func (p IntegrationRuntimePolicy) CheckIntegrationAccessDecision(r port.IntegrationAccessInput, d port.IntegrationAccessDecision, current port.IntegrationAccessInput) error {
	expected, err := p.DecideIntegrationAccess(r)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expected, d) {
		return port.ErrIntegrationDecisionMismatch
	}
	if d.Effect != "allow" {
		return &port.IntegrationAccessDenied{Reason: d.Reason}
	}
	if runtimeShape(current) != nil || current.NowMS < r.NowMS {
		return port.ErrIntegrationFactsChanged
	}
	same := current.Clone()
	same.NowMS = r.NowMS
	if !reflect.DeepEqual(same, r) {
		return port.ErrIntegrationFactsChanged
	}
	fresh, err := p.DecideIntegrationAccess(current)
	if err != nil {
		return err
	}
	if fresh.Effect != "allow" {
		return &port.IntegrationAccessDenied{Reason: fresh.Reason}
	}
	if fresh.RuleID != d.RuleID {
		return port.ErrIntegrationFactsChanged
	}
	return nil
}
