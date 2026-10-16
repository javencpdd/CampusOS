package policy

import (
	"testing"

	"github.com/campusos/CampusOS/internal/modules/core/identity/port"
)

func TestThreadOwnerLifecycleRawStatesAndDomains(t *testing.T) {
	for _, op := range []string{"trash", "restore", "submit_review"} {
		for _, pub := range []string{"draft", "private", "published"} {
			for _, mod := range []string{"clear", "pending", "rejected", "taken_down"} {
				for _, del := range []string{"active", "trashed", "purged"} {
					for _, kind := range []string{"user", "admin", "anonymous", "plugin_instance"} {
						r := port.ThreadOwnerWriteRequest{Contract: port.ThreadOwnerWriteContract, RequestID: "lifecycle-request", PolicyVersion: port.ThreadOwnerWritePolicyVersion, Principal: ownerReadPrincipal(kind), Facts: ownerReadRequest().Facts, Operation: op}
						r.Facts.PublicationStatus, r.Facts.ModerationStatus, r.Facts.DeletionStatus = pub, mod, del
						d, err := (ThreadOwnerWritePolicy{}).DecideThreadOwnerWrite(r)
						if err != nil {
							t.Fatal(err)
						}
						allow := kind == "user" && ((op == "trash" && del == "active") || (op == "restore" && del == "trashed") || (op == "submit_review" && del == "active" && (mod == "rejected" || mod == "taken_down")))
						if (d.Effect == "allow") != allow {
							t.Fatalf("%s/%s/%s/%s/%s %+v", op, pub, mod, del, kind, d)
						}
						if allow {
							if len(d.Obligations) != 2 || d.Obligations[1] != "required_audit" {
								t.Fatal(d)
							}
							if err := CheckThreadOwnerWriteDecision(r, d, r); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			}
		}
	}
}
func TestThreadOwnerLifecycleBindingAndUnknownAction(t *testing.T) {
	r := port.ThreadOwnerWriteRequest{Contract: port.ThreadOwnerWriteContract, RequestID: "lifecycle-request", PolicyVersion: port.ThreadOwnerWritePolicyVersion, Principal: ownerReadPrincipal("user"), Facts: ownerReadRequest().Facts, Operation: "restore"}
	r.Facts.DeletionStatus = "trashed"
	d, err := (ThreadOwnerWritePolicy{}).DecideThreadOwnerWrite(r)
	if err != nil || d.Effect != "allow" {
		t.Fatal(d, err)
	}
	for _, mode := range []string{"owner", "actor", "board", "state", "version", "credential", "operation"} {
		current := r
		switch mode {
		case "owner":
			current.Facts.Owner.ID = "other"
		case "actor":
			current.Principal.Actor.ID = "other"
		case "board":
			current.Facts.Board.ID = "other"
		case "state":
			current.Facts.DeletionStatus = "active"
		case "version":
			current.Facts.FactsVersion = "different"
		case "credential":
			current.Principal.CredentialID = "replacement"
		case "operation":
			current.Operation = "trash"
		}
		if err := CheckThreadOwnerWriteDecision(r, d, current); err == nil {
			t.Fatalf("stale %s accepted", mode)
		}
	}
	r.Operation = "take_down"
	if _, err := (ThreadOwnerWritePolicy{}).DecideThreadOwnerWrite(r); err == nil {
		t.Fatal("unknown lifecycle action accepted")
	}
}
