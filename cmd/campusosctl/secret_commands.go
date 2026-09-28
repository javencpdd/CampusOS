package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/campusos/CampusOS/internal/platform/reliability"
	"github.com/campusos/CampusOS/internal/plugin"
	"github.com/jackc/pgx/v5/pgxpool"
)

const secretRewrapUsage = "usage: campusosctl secret rewrap --plugin-id N [--batch-size 1..500] [--apply --expected-count N --actor ID --reason TEXT]"

const secretRewrapOperationKind = "plugin.secret_rewrap"

type secretRewrapOptions struct {
	PluginID      int64
	BatchSize     int
	Apply         bool
	ExpectedCount int64
	Actor         string
	Reason        string
}

type secretRewrapResult struct {
	PluginID    int64  `json:"plugin_id"`
	ActiveKeyID string `json:"active_key_id"`
	DryRun      bool   `json:"dry_run"`
	Eligible    int64  `json:"eligible"`
	Selected    int    `json:"selected"`
	Rewrapped   int    `json:"rewrapped"`
	Remaining   int64  `json:"remaining"`
	Complete    bool   `json:"complete"`
	AuditID     string `json:"audit_id,omitempty"`
}

// Details deliberately contain only operator intent and aggregate counts.
// The Secret row identity, value, envelope and database address stay out of
// both the operation record and command output.
type secretRewrapAuditDetails struct {
	PluginID      int64  `json:"plugin_id"`
	BatchSize     int    `json:"batch_size"`
	Reason        string `json:"reason"`
	ExpectedCount int64  `json:"expected_count"`
	Eligible      int64  `json:"eligible"`
	Selected      int    `json:"selected"`
	Rewrapped     int    `json:"rewrapped"`
	Remaining     *int64 `json:"remaining,omitempty"`
}

func runSecret(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printSecretUsage(stderr)
		return 2
	}
	switch args[0] {
	case "inspect-key":
		if len(args) == 2 && (args[1] == "help" || args[1] == "-h" || args[1] == "--help") {
			printSecretUsage(stdout)
			return 0
		}
		if err := runSecretInspectKey(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "secret inspect-key: %v\n", err)
			return 1
		}
		return 0
	case "rewrap":
		if len(args) == 2 && (args[1] == "help" || args[1] == "-h" || args[1] == "--help") {
			printSecretUsage(stdout)
			return 0
		}
		if err := runSecretRewrap(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "secret rewrap: %v\n", err)
			return 1
		}
		return 0
	case "help", "-h", "--help":
		printSecretUsage(stdout)
		return 0
	default:
		fmt.Fprintln(stderr, "unknown secret command")
		printSecretUsage(stderr)
		return 2
	}
}

func printSecretUsage(writer io.Writer) {
	fmt.Fprintln(writer, secretRewrapUsage)
	fmt.Fprintln(writer, secretInspectKeyUsage)
	fmt.Fprintln(writer, "")
	fmt.Fprintln(writer, "Defaults to a read-only preview. Apply processes at most one bounded batch after the current candidate count matches --expected-count and an operation audit is recorded. DATABASE_DSN and both CAMPUSOS_SECRET_* keyring variables must be set in the local environment.")
}

func parseSecretRewrapOptions(args []string) (secretRewrapOptions, error) {
	flags := flag.NewFlagSet("secret rewrap", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	pluginID := flags.Int64("plugin-id", 0, "target plugin ID")
	batchSize := flags.Int("batch-size", 100, "maximum rows in one apply (1..500)")
	apply := flags.Bool("apply", false, "perform one bounded rewrap batch")
	expectedCount := flags.Int64("expected-count", -1, "current eligible count required with --apply")
	actor := flags.String("actor", "", "local operator identity required with --apply")
	reason := flags.String("reason", "", "operator reason required with --apply")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return secretRewrapOptions{}, errors.New(secretRewrapUsage)
	}
	if *pluginID <= 0 {
		return secretRewrapOptions{}, errors.New("--plugin-id must be a positive ID")
	}
	if *batchSize < 1 || *batchSize > 500 {
		return secretRewrapOptions{}, errors.New("--batch-size must be between 1 and 500")
	}
	options := secretRewrapOptions{PluginID: *pluginID, BatchSize: *batchSize, Apply: *apply, ExpectedCount: *expectedCount}
	provided := map[string]bool{}
	flags.Visit(func(item *flag.Flag) { provided[item.Name] = true })
	if !options.Apply {
		if provided["expected-count"] || provided["actor"] || provided["reason"] {
			return secretRewrapOptions{}, errors.New("--expected-count, --actor and --reason require --apply")
		}
		return options, nil
	}
	if *expectedCount < 0 {
		return secretRewrapOptions{}, errors.New("--apply requires a non-negative --expected-count")
	}
	options.Actor = strings.TrimSpace(*actor)
	options.Reason = strings.TrimSpace(*reason)
	if !validSecretRewrapAuditText(options.Actor, 64) || len(options.Actor) > 64 {
		return secretRewrapOptions{}, errors.New("--apply requires an --actor of 1 to 64 printable bytes")
	}
	if !validSecretRewrapAuditText(options.Reason, 500) {
		return secretRewrapOptions{}, errors.New("--apply requires a --reason of 1 to 500 printable characters")
	}
	return options, nil
}

func validSecretRewrapAuditText(value string, maxRunes int) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxRunes {
		return false
	}
	for _, ch := range value {
		if unicode.IsControl(ch) {
			return false
		}
	}
	return true
}

func runSecretRewrap(args []string, stdout io.Writer) error {
	options, err := parseSecretRewrapOptions(args)
	if err != nil {
		return err
	}
	if strings.TrimSpace(os.Getenv("DATABASE_DSN")) == "" {
		return errors.New("DATABASE_DSN is required")
	}
	if strings.TrimSpace(os.Getenv("CAMPUSOS_SECRET_ACTIVE_KEY_ID")) == "" || strings.TrimSpace(os.Getenv("CAMPUSOS_SECRET_ENCRYPTION_KEYS")) == "" {
		return errors.New("CAMPUSOS_SECRET_ACTIVE_KEY_ID and CAMPUSOS_SECRET_ENCRYPTION_KEYS are required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, strings.TrimSpace(os.Getenv("DATABASE_DSN")))
	if err != nil {
		return errors.New("connect secret database")
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return errors.New("connect secret database")
	}
	store := plugin.NewPgAuthorizationStore(pool)
	service, err := plugin.NewSecretServiceFromEnv(store)
	if err != nil {
		return errors.New("configure secret keyring")
	}
	activeKeyID := service.ActiveKeyID()
	eligible, err := store.CountActiveSecretRewrapCandidates(ctx, options.PluginID, activeKeyID)
	if err != nil {
		return errors.New("count secret rewrap candidates")
	}
	result := secretRewrapResult{
		PluginID: options.PluginID, ActiveKeyID: activeKeyID, DryRun: !options.Apply,
		Eligible: eligible, Remaining: eligible, Complete: eligible == 0,
	}
	if !options.Apply {
		result.Selected = options.BatchSize
		if eligible < int64(result.Selected) {
			result.Selected = int(eligible)
		}
		return writeSecretRewrapResult(stdout, result)
	}
	if eligible != options.ExpectedCount {
		return errors.New("--expected-count does not match the current eligible count; rerun the preview")
	}

	auditStore := reliability.NewPostgreSQLStore(pool)
	details := secretRewrapAuditDetails{
		PluginID: options.PluginID, BatchSize: options.BatchSize, Reason: options.Reason,
		ExpectedCount: options.ExpectedCount, Eligible: eligible, Remaining: &eligible,
	}
	audit, err := auditStore.StartOperation(ctx, reliability.Operation{
		Kind: secretRewrapOperationKind, SubjectType: "plugin", SubjectID: fmt.Sprint(options.PluginID),
		Status: reliability.OperationRunning, ActorID: options.Actor, Details: mustSecretRewrapDetails(details),
	})
	if err != nil {
		return errors.New("start secret rewrap audit")
	}
	result.AuditID = audit.ID
	batch, batchErr := plugin.RewrapActiveSecretBatch(ctx, service, store, options.PluginID, options.BatchSize)
	result.Selected = batch.Selected
	result.Rewrapped = batch.Rewrapped
	details.Selected = batch.Selected
	details.Rewrapped = batch.Rewrapped
	// A canceled batch context must not prevent a best-effort durable failure
	// record. The start row is already committed before any Secret mutation.
	finalizeCtx, finalizeCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer finalizeCancel()
	if batchErr == nil {
		result.Remaining = batch.Remaining
		result.Complete = batch.Remaining == 0
		details.Remaining = &batch.Remaining
		audit.Status = reliability.OperationSucceeded
	} else {
		// A batch can have committed earlier rows before a later row fails.
		// Recount outside its possibly canceled context for an honest audit.
		details.Remaining = nil
		if remaining, countErr := store.CountActiveSecretRewrapCandidates(finalizeCtx, options.PluginID, activeKeyID); countErr == nil {
			details.Remaining = &remaining
		}
		audit.Status = reliability.OperationFailed
		audit.Error = "secret rewrap failed"
	}
	audit.Details = mustSecretRewrapDetails(details)
	if err := auditStore.UpdateOperation(finalizeCtx, *audit); err != nil {
		return errors.New("finish secret rewrap audit")
	}
	if batchErr != nil {
		return errors.New("secret rewrap batch failed; inspect the operation audit")
	}
	return writeSecretRewrapResult(stdout, result)
}

func mustSecretRewrapDetails(details secretRewrapAuditDetails) json.RawMessage {
	encoded, _ := json.Marshal(details)
	return encoded
}

func writeSecretRewrapResult(writer io.Writer, result secretRewrapResult) error {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return errors.New("write secret rewrap result")
	}
	return nil
}
