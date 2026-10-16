package port

import "reflect"

const AdminSelfReadPolicyVersion = "admin-self-read-1"

type AdminSelfReadRequest struct {
	RequestID, PolicyVersion                                   string
	Principal                                                  PrincipalContext
	AccountID, Status, SessionID                               string
	AccountVersion, SessionVersion, ExpiresAtMS, EvaluatedAtMS int64
}

func (r AdminSelfReadRequest) Validate() error {
	if !validOpaqueID(r.RequestID) || r.PolicyVersion != AdminSelfReadPolicyVersion || r.Principal.Validate() != nil || !validOpaqueID(r.AccountID) || !validOpaqueID(r.SessionID) || r.AccountVersion < 1 || r.SessionVersion < 1 || r.ExpiresAtMS < 1 || r.EvaluatedAtMS < 1 {
		return ErrPolicyDecisionMismatch
	}
	if r.Status != "active" && r.Status != "suspended" && r.Status != "revoked" {
		return ErrPolicyDecisionMismatch
	}
	return nil
}

type AdminSelfReadDecision struct {
	Request AdminSelfReadRequest
	Effect  string
}

func CheckAdminSelfReadDecision(r AdminSelfReadRequest, d AdminSelfReadDecision, current AdminSelfReadRequest) error {
	if r.Validate() != nil || current.Validate() != nil || d.Effect != "allow" || !reflect.DeepEqual(d.Request, r) {
		return ErrPolicyDecisionMismatch
	}
	if !reflect.DeepEqual(r, current) {
		return ErrPolicyFactsChanged
	}
	if r.Principal.Actor.Kind != "admin" || r.Principal.Audience != "admin" || r.Principal.Actor.ID != r.AccountID || r.Principal.CredentialID != r.SessionID || r.Status != "active" || r.EvaluatedAtMS >= r.ExpiresAtMS {
		return ErrPolicyDecisionMismatch
	}
	return nil
}
