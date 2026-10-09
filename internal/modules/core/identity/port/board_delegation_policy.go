package port

import (
	"context"
	"encoding/json"
	"errors"
)

const BoardDelegationContract = "campusos.board-delegation/v1"

// boardDelegationMaxTime is the G1 contract's Unix-second ceiling
// (9999-12-31T23:59:59Z). Windows are exact half-open intervals.
const boardDelegationMaxTime = 253402300799

var (
	ErrDelegationInputInvalid        = errors.New("delegation.input_invalid")
	ErrDelegationContractUnsupported = errors.New("delegation.contract_unsupported")
	ErrDelegationDecisionMismatch    = errors.New("delegation.decision_mismatch")
	ErrDelegationFactsChanged        = errors.New("delegation.facts_changed")
)

// DelegationDenyError carries the stable reason of a deny decision that was
// verified against the original evaluation during consumption. It is not a
// mismatch: the proposal stays denied with its original reason code.
type DelegationDenyError struct {
	Reason string
}

func (e *DelegationDenyError) Error() string { return e.Reason }

// BoardGrantAtom is one candidate execution right. It carries no delegable
// flag: a granted candidate never gives the recipient re-delegation power.
type BoardGrantAtom struct {
	Action           string `json:"action"`
	BoardID          string `json:"board_id"`
	NotBefore        int64  `json:"not_before"`
	ExpiresAt        int64  `json:"expires_at"`
	RequiredStrength string `json:"required_strength"`
}

// BoardDelegationBound is one delegable ceiling granted to an admin actor.
// A candidate atom is covered only when a single current bound witnesses
// action, exact board, window containment and authentication strength.
type BoardDelegationBound struct {
	Action           string       `json:"action"`
	BoardID          string       `json:"board_id"`
	NotBefore        int64        `json:"not_before"`
	ExpiresAt        int64        `json:"expires_at"`
	RequiredStrength string       `json:"required_strength"`
	ID               string       `json:"id"`
	Actor            PrincipalRef `json:"actor"`
	Status           string       `json:"status"`
	Delegable        bool         `json:"delegable"`
}

// BoardDelegationManagement binds the actor's identity.role.assign grant.
// Holding governance execution rights does not imply it and vice versa.
type BoardDelegationManagement struct {
	Actor     PrincipalRef `json:"actor"`
	Action    string       `json:"action"`
	Status    string       `json:"status"`
	NotBefore int64        `json:"not_before"`
	ExpiresAt int64        `json:"expires_at"`
}

type BoardDelegationRecipient struct {
	Subject PrincipalRef `json:"subject"`
	Status  string       `json:"status"`
}

type BoardDelegationBoard struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Status string `json:"status"`
}

type BoardDelegationCandidate struct {
	Recipient PrincipalRef     `json:"recipient"`
	Grants    []BoardGrantAtom `json:"grants"`
}

// BoardDelegationRequest is constructed by the host from current facts. The
// browser may only propose candidate intent; bounds, management, the clock
// and facts_version are never client-supplied trusted values.
type BoardDelegationRequest struct {
	Contract     string                    `json:"contract"`
	RequestID    string                    `json:"request_id"`
	FactsVersion string                    `json:"facts_version"`
	EvaluatedAt  int64                     `json:"evaluated_at"`
	Principal    PrincipalContext          `json:"principal"`
	Management   BoardDelegationManagement `json:"management"`
	Recipient    BoardDelegationRecipient  `json:"recipient"`
	Boards       []BoardDelegationBoard    `json:"boards"`
	Bounds       []BoardDelegationBound    `json:"bounds"`
	Candidate    BoardDelegationCandidate  `json:"candidate"`
}

type BoardDelegationWitness struct {
	CandidateIndex int    `json:"candidate_index"`
	BoundID        string `json:"bound_id"`
}

// BoardDelegationDecision is an all-or-nothing proposal verdict. An allow is
// not a committed grant: the caller must re-read authority, version and clock
// inside the write transaction and fulfill every returned obligation.
type BoardDelegationDecision struct {
	Contract     string                   `json:"contract"`
	RequestID    string                   `json:"request_id"`
	FactsVersion string                   `json:"facts_version"`
	Effect       string                   `json:"effect"`
	Reason       string                   `json:"reason"`
	Witnesses    []BoardDelegationWitness `json:"witnesses"`
	Obligations  []string                 `json:"obligations"`
}

// BoardFactsProvider reloads the current facts of the candidate boards. The
// calling host module owns category state; the delegation service invokes it
// once before evaluation and once more inside the write transaction.
type BoardFactsProvider func(ctx context.Context) ([]BoardDelegationBoard, error)

// BoardDelegationPolicy evaluates delegation proposals from host-supplied
// current facts only. It neither authenticates credentials nor loads
// management/bounds/recipient/board state, and it never commits writes.
type BoardDelegationPolicy interface {
	DecideBoardDelegation(BoardDelegationRequest) (BoardDelegationDecision, error)
}

func knownDelegationAction(action string) bool {
	return action == "community.thread.take_down" || action == "community.post.delete"
}

func validDelegationTime(value int64) bool {
	return value >= 0 && value <= boardDelegationMaxTime
}

func validDelegationWindow(notBefore, expiresAt int64) bool {
	return validDelegationTime(notBefore) && validDelegationTime(expiresAt) && notBefore < expiresAt
}

func validDelegationStrength(strength string) bool {
	return strength == "password" || strength == "mfa"
}

func validDelegationRef(ref PrincipalRef, kind string) bool {
	return ref.Kind == kind && validOpaqueID(ref.ID)
}

func (g BoardGrantAtom) Validate() error {
	if !knownDelegationAction(g.Action) || !validOpaqueID(g.BoardID) ||
		!validDelegationWindow(g.NotBefore, g.ExpiresAt) || !validDelegationStrength(g.RequiredStrength) {
		return ErrDelegationInputInvalid
	}
	return nil
}

func (b BoardDelegationBound) Validate() error {
	if (BoardGrantAtom{Action: b.Action, BoardID: b.BoardID, NotBefore: b.NotBefore, ExpiresAt: b.ExpiresAt, RequiredStrength: b.RequiredStrength}).Validate() != nil ||
		!validOpaqueID(b.ID) || !validDelegationRef(b.Actor, "admin") ||
		(b.Status != "active" && b.Status != "suspended" && b.Status != "revoked") {
		return ErrDelegationInputInvalid
	}
	return nil
}

func (m BoardDelegationManagement) Validate() error {
	if !validDelegationRef(m.Actor, "admin") || m.Action != "identity.role.assign" ||
		(m.Status != "active" && m.Status != "suspended" && m.Status != "revoked") ||
		!validDelegationWindow(m.NotBefore, m.ExpiresAt) {
		return ErrDelegationInputInvalid
	}
	return nil
}

func (r BoardDelegationRecipient) Validate() error {
	if !validDelegationRef(r.Subject, "user") ||
		(r.Status != "active" && r.Status != "suspended" && r.Status != "deleted") {
		return ErrDelegationInputInvalid
	}
	return nil
}

func (b BoardDelegationBoard) Validate() error {
	if b.Kind != "community.board" || !validOpaqueID(b.ID) || (b.Status != "active" && b.Status != "archived") {
		return ErrDelegationInputInvalid
	}
	return nil
}

func (c BoardDelegationCandidate) Validate() error {
	if !validDelegationRef(c.Recipient, "user") || c.Grants == nil || len(c.Grants) < 1 || len(c.Grants) > 32 {
		return ErrDelegationInputInvalid
	}
	pairs := make(map[[2]string]bool, len(c.Grants))
	for _, g := range c.Grants {
		if g.Validate() != nil {
			return ErrDelegationInputInvalid
		}
		pair := [2]string{g.Action, g.BoardID}
		if pairs[pair] {
			return ErrDelegationInputInvalid
		}
		pairs[pair] = true
	}
	return nil
}

// Validate enforces the structural G1 request schema plus the cross-field
// semantic rules: finite ordered windows, unique board/bound IDs and unique
// candidate action/board pairs. An empty contract is invalid; any other
// mismatched contract string is unsupported.
func (r BoardDelegationRequest) Validate() error {
	if r.Contract != BoardDelegationContract {
		if r.Contract == "" {
			return ErrDelegationInputInvalid
		}
		return ErrDelegationContractUnsupported
	}
	if !validOpaqueID(r.RequestID) || !validOpaqueID(r.FactsVersion) || !validDelegationTime(r.EvaluatedAt) ||
		r.Principal.Validate() != nil || r.Management.Validate() != nil || r.Recipient.Validate() != nil ||
		r.Boards == nil || len(r.Boards) < 1 || len(r.Boards) > 64 ||
		r.Bounds == nil || len(r.Bounds) > 64 || r.Candidate.Validate() != nil {
		return ErrDelegationInputInvalid
	}
	boardIDs := make(map[string]bool, len(r.Boards))
	for _, b := range r.Boards {
		if b.Validate() != nil || boardIDs[b.ID] {
			return ErrDelegationInputInvalid
		}
		boardIDs[b.ID] = true
	}
	boundIDs := make(map[string]bool, len(r.Bounds))
	for _, b := range r.Bounds {
		if b.Validate() != nil || boundIDs[b.ID] {
			return ErrDelegationInputInvalid
		}
		boundIDs[b.ID] = true
	}
	return nil
}

// Validate mirrors the G1 decision schema: an allow needs at least one unique
// witness and exactly the recheck/audit obligation pair; a deny carries
// neither. Reason codes stay within the registered delegation catalog.
func (d BoardDelegationDecision) Validate() error {
	if d.Contract != BoardDelegationContract || !validOpaqueID(d.RequestID) || !validOpaqueID(d.FactsVersion) ||
		d.Witnesses == nil || len(d.Witnesses) > 32 || d.Obligations == nil {
		return ErrDelegationInputInvalid
	}
	seen := make(map[BoardDelegationWitness]bool, len(d.Witnesses))
	for _, w := range d.Witnesses {
		if w.CandidateIndex < 0 || w.CandidateIndex > 31 || !validOpaqueID(w.BoundID) || seen[w] {
			return ErrDelegationInputInvalid
		}
		seen[w] = true
	}
	switch d.Effect {
	case "allow":
		if d.Reason != "delegation.allowed" || len(d.Witnesses) < 1 || len(d.Obligations) != 2 ||
			d.Obligations[0] != "recheck_authority_in_transaction" || d.Obligations[1] != "required_audit" {
			return ErrDelegationInputInvalid
		}
	case "deny":
		switch d.Reason {
		case "delegation.actor_denied", "delegation.mfa_required", "delegation.management_denied",
			"delegation.recipient_denied", "delegation.board_denied", "delegation.outside_bounds":
		default:
			return ErrDelegationInputInvalid
		}
		if len(d.Witnesses) != 0 || len(d.Obligations) != 0 {
			return ErrDelegationInputInvalid
		}
	default:
		return ErrDelegationInputInvalid
	}
	return nil
}

// delegationUnixSeconds decodes an exact integer in the contract's Unix-second
// range. Fractional, exponential-overflow and out-of-range values are rejected,
// never rounded into a grant window.
func delegationUnixSeconds(raw []byte) (int64, error) {
	value, err := resourcePolicyMillis(raw)
	if err != nil || value > boardDelegationMaxTime {
		return 0, ErrDelegationInputInvalid
	}
	return value, nil
}

func parseDelegationRef(raw []byte, kind string) (PrincipalRef, error) {
	fields, err := strictObject(raw, []string{"kind", "id"}, nil)
	if err != nil {
		return PrincipalRef{}, ErrDelegationInputInvalid
	}
	var ref PrincipalRef
	for key, dst := range map[string]*string{"kind": &ref.Kind, "id": &ref.ID} {
		if json.Unmarshal(fields[key], dst) != nil {
			return PrincipalRef{}, ErrDelegationInputInvalid
		}
	}
	if !validDelegationRef(ref, kind) {
		return PrincipalRef{}, ErrDelegationInputInvalid
	}
	return ref, nil
}

func parseDelegationGrantAtom(raw []byte) (BoardGrantAtom, error) {
	fields, err := strictObject(raw, []string{"action", "board_id", "not_before", "expires_at", "required_strength"}, nil)
	if err != nil {
		return BoardGrantAtom{}, ErrDelegationInputInvalid
	}
	var g BoardGrantAtom
	for key, dst := range map[string]*string{"action": &g.Action, "board_id": &g.BoardID, "required_strength": &g.RequiredStrength} {
		if json.Unmarshal(fields[key], dst) != nil {
			return BoardGrantAtom{}, ErrDelegationInputInvalid
		}
	}
	if g.NotBefore, err = delegationUnixSeconds(fields["not_before"]); err != nil {
		return BoardGrantAtom{}, err
	}
	if g.ExpiresAt, err = delegationUnixSeconds(fields["expires_at"]); err != nil {
		return BoardGrantAtom{}, err
	}
	if err = g.Validate(); err != nil {
		return BoardGrantAtom{}, err
	}
	return g, nil
}

func parseDelegationBound(raw []byte) (BoardDelegationBound, error) {
	fields, err := strictObject(raw, []string{"action", "board_id", "not_before", "expires_at", "required_strength", "id", "actor", "status", "delegable"}, nil)
	if err != nil {
		return BoardDelegationBound{}, ErrDelegationInputInvalid
	}
	var b BoardDelegationBound
	for key, dst := range map[string]*string{"action": &b.Action, "board_id": &b.BoardID, "required_strength": &b.RequiredStrength, "id": &b.ID, "status": &b.Status} {
		if json.Unmarshal(fields[key], dst) != nil {
			return BoardDelegationBound{}, ErrDelegationInputInvalid
		}
	}
	if b.NotBefore, err = delegationUnixSeconds(fields["not_before"]); err != nil {
		return BoardDelegationBound{}, err
	}
	if b.ExpiresAt, err = delegationUnixSeconds(fields["expires_at"]); err != nil {
		return BoardDelegationBound{}, err
	}
	if b.Actor, err = parseDelegationRef(fields["actor"], "admin"); err != nil {
		return BoardDelegationBound{}, err
	}
	if json.Unmarshal(fields["delegable"], &b.Delegable) != nil || b.Validate() != nil {
		return BoardDelegationBound{}, ErrDelegationInputInvalid
	}
	return b, nil
}

// ParseBoardDelegationRequestJSON is a bounded host codec, not an ingress
// authorization endpoint. An unknown contract string wins over other input
// errors; any schema or semantic violation is input_invalid without echo.
func ParseBoardDelegationRequestJSON(raw []byte) (BoardDelegationRequest, error) {
	if !resourceJSONUnicodeValid(raw) {
		return BoardDelegationRequest{}, ErrDelegationInputInvalid
	}
	fields, err := decodeStrictObject(raw)
	if err != nil {
		return BoardDelegationRequest{}, ErrDelegationInputInvalid
	}
	if value, exists := fields["contract"]; exists {
		var version string
		if json.Unmarshal(value, &version) == nil && version != BoardDelegationContract {
			var scalar any
			if json.Unmarshal(value, &scalar) == nil {
				if _, ok := scalar.(string); ok {
					return BoardDelegationRequest{}, ErrDelegationContractUnsupported
				}
			}
		}
	}
	fields, err = strictObject(raw, []string{"contract", "request_id", "facts_version", "evaluated_at", "principal", "management", "recipient", "boards", "bounds", "candidate"}, nil)
	if err != nil {
		return BoardDelegationRequest{}, ErrDelegationInputInvalid
	}
	var r BoardDelegationRequest
	for key, dst := range map[string]*string{"contract": &r.Contract, "request_id": &r.RequestID, "facts_version": &r.FactsVersion} {
		if json.Unmarshal(fields[key], dst) != nil {
			return BoardDelegationRequest{}, ErrDelegationInputInvalid
		}
	}
	if r.EvaluatedAt, err = delegationUnixSeconds(fields["evaluated_at"]); err != nil {
		return BoardDelegationRequest{}, err
	}
	if r.Principal, err = ParsePrincipalJSON(fields["principal"]); err != nil {
		return BoardDelegationRequest{}, ErrDelegationInputInvalid
	}
	if r.Management, err = parseDelegationManagement(fields["management"]); err != nil {
		return BoardDelegationRequest{}, err
	}
	if r.Recipient, err = parseDelegationRecipient(fields["recipient"]); err != nil {
		return BoardDelegationRequest{}, err
	}
	if r.Boards, err = parseDelegationBoards(fields["boards"]); err != nil {
		return BoardDelegationRequest{}, err
	}
	if r.Bounds, err = parseDelegationBounds(fields["bounds"]); err != nil {
		return BoardDelegationRequest{}, err
	}
	if r.Candidate, err = parseDelegationCandidate(fields["candidate"]); err != nil {
		return BoardDelegationRequest{}, err
	}
	if err = r.Validate(); err != nil {
		return BoardDelegationRequest{}, err
	}
	return r, nil
}

func parseDelegationManagement(raw []byte) (BoardDelegationManagement, error) {
	fields, err := strictObject(raw, []string{"actor", "action", "status", "not_before", "expires_at"}, nil)
	if err != nil {
		return BoardDelegationManagement{}, ErrDelegationInputInvalid
	}
	var m BoardDelegationManagement
	for key, dst := range map[string]*string{"action": &m.Action, "status": &m.Status} {
		if json.Unmarshal(fields[key], dst) != nil {
			return BoardDelegationManagement{}, ErrDelegationInputInvalid
		}
	}
	if m.Actor, err = parseDelegationRef(fields["actor"], "admin"); err != nil {
		return BoardDelegationManagement{}, err
	}
	if m.NotBefore, err = delegationUnixSeconds(fields["not_before"]); err != nil {
		return BoardDelegationManagement{}, err
	}
	if m.ExpiresAt, err = delegationUnixSeconds(fields["expires_at"]); err != nil {
		return BoardDelegationManagement{}, err
	}
	if err = m.Validate(); err != nil {
		return BoardDelegationManagement{}, err
	}
	return m, nil
}

func parseDelegationRecipient(raw []byte) (BoardDelegationRecipient, error) {
	fields, err := strictObject(raw, []string{"subject", "status"}, nil)
	if err != nil {
		return BoardDelegationRecipient{}, ErrDelegationInputInvalid
	}
	var r BoardDelegationRecipient
	if r.Subject, err = parseDelegationRef(fields["subject"], "user"); err != nil {
		return BoardDelegationRecipient{}, err
	}
	if json.Unmarshal(fields["status"], &r.Status) != nil || r.Validate() != nil {
		return BoardDelegationRecipient{}, ErrDelegationInputInvalid
	}
	return r, nil
}

func parseDelegationBoards(raw []byte) ([]BoardDelegationBoard, error) {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil || len(items) < 1 || len(items) > 64 {
		return nil, ErrDelegationInputInvalid
	}
	boards := make([]BoardDelegationBoard, 0, len(items))
	for _, item := range items {
		fields, err := strictObject(item, []string{"kind", "id", "status"}, nil)
		if err != nil {
			return nil, ErrDelegationInputInvalid
		}
		var b BoardDelegationBoard
		for key, dst := range map[string]*string{"kind": &b.Kind, "id": &b.ID, "status": &b.Status} {
			if json.Unmarshal(fields[key], dst) != nil {
				return nil, ErrDelegationInputInvalid
			}
		}
		if err = b.Validate(); err != nil {
			return nil, err
		}
		boards = append(boards, b)
	}
	return boards, nil
}

func parseDelegationBounds(raw []byte) ([]BoardDelegationBound, error) {
	var items []json.RawMessage
	if json.Unmarshal(raw, &items) != nil || items == nil || len(items) > 64 {
		return nil, ErrDelegationInputInvalid
	}
	bounds := make([]BoardDelegationBound, 0, len(items))
	for _, item := range items {
		b, err := parseDelegationBound(item)
		if err != nil {
			return nil, err
		}
		bounds = append(bounds, b)
	}
	return bounds, nil
}

func parseDelegationCandidate(raw []byte) (BoardDelegationCandidate, error) {
	fields, err := strictObject(raw, []string{"recipient", "grants"}, nil)
	if err != nil {
		return BoardDelegationCandidate{}, ErrDelegationInputInvalid
	}
	var c BoardDelegationCandidate
	if c.Recipient, err = parseDelegationRef(fields["recipient"], "user"); err != nil {
		return BoardDelegationCandidate{}, err
	}
	var items []json.RawMessage
	if json.Unmarshal(fields["grants"], &items) != nil || items == nil || len(items) < 1 || len(items) > 32 {
		return BoardDelegationCandidate{}, ErrDelegationInputInvalid
	}
	c.Grants = make([]BoardGrantAtom, 0, len(items))
	for _, item := range items {
		g, err := parseDelegationGrantAtom(item)
		if err != nil {
			return BoardDelegationCandidate{}, err
		}
		c.Grants = append(c.Grants, g)
	}
	if err = c.Validate(); err != nil {
		return BoardDelegationCandidate{}, err
	}
	return c, nil
}

func ParseBoardDelegationDecisionJSON(raw []byte) (BoardDelegationDecision, error) {
	if !resourceJSONUnicodeValid(raw) {
		return BoardDelegationDecision{}, ErrDelegationInputInvalid
	}
	fields, err := strictObject(raw, []string{"contract", "request_id", "facts_version", "effect", "reason", "witnesses", "obligations"}, nil)
	if err != nil {
		return BoardDelegationDecision{}, ErrDelegationInputInvalid
	}
	var d BoardDelegationDecision
	for key, dst := range map[string]*string{"contract": &d.Contract, "request_id": &d.RequestID, "facts_version": &d.FactsVersion, "effect": &d.Effect, "reason": &d.Reason} {
		if json.Unmarshal(fields[key], dst) != nil {
			return BoardDelegationDecision{}, ErrDelegationInputInvalid
		}
	}
	var items []json.RawMessage
	if json.Unmarshal(fields["witnesses"], &items) != nil || items == nil || len(items) > 32 {
		return BoardDelegationDecision{}, ErrDelegationInputInvalid
	}
	d.Witnesses = make([]BoardDelegationWitness, 0, len(items))
	for _, item := range items {
		pair, err := strictObject(item, []string{"candidate_index", "bound_id"}, nil)
		if err != nil {
			return BoardDelegationDecision{}, ErrDelegationInputInvalid
		}
		var w BoardDelegationWitness
		index, err := delegationUnixSeconds(pair["candidate_index"])
		if err != nil || index > 31 {
			return BoardDelegationDecision{}, ErrDelegationInputInvalid
		}
		w.CandidateIndex = int(index)
		if json.Unmarshal(pair["bound_id"], &w.BoundID) != nil {
			return BoardDelegationDecision{}, ErrDelegationInputInvalid
		}
		d.Witnesses = append(d.Witnesses, w)
	}
	if json.Unmarshal(fields["obligations"], &d.Obligations) != nil || d.Obligations == nil {
		return BoardDelegationDecision{}, ErrDelegationInputInvalid
	}
	if err = d.Validate(); err != nil {
		return BoardDelegationDecision{}, err
	}
	return d, nil
}
