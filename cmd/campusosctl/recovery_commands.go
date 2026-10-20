package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/campusos/CampusOS/internal/operations/recovery"
)

func runRecovery(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: campusosctl recovery <verify|extract|apply|status|review> [flags]")
		return 2
	}
	action := args[0]
	flags := flag.NewFlagSet("recovery "+action, flag.ContinueOnError)
	flags.SetOutput(stderr)
	archive := flags.String("archive", "", "trusted v3 backup")
	destination := flags.String("destination", "", "new or empty private extraction directory")
	root := flags.String("data-root", "data", "offline persistent data root")
	database := flags.String("database", "", "exact target database name")
	migrations := flags.String("migrations", "migrations", "prebuilt runtime migration directory")
	confirm := flags.Bool("confirm-offline", false, "all API and external writers are stopped")
	epoch := flags.String("epoch", "", "current quarantined authentication epoch")
	inventory := flags.String("inventory-sha256", "", "exact locally reviewed authority inventory SHA-256")
	actor := flags.String("operator", "", "trusted local operator reference")
	reason := flags.String("reason", "", "authorization review reference/reason (no secrets)")
	maxBytes := flags.Int64("max-bytes", recovery.DefaultLimits.Bytes, "maximum uncompressed bytes per layer")
	maxEntries := flags.Int("max-entries", recovery.DefaultLimits.Entries, "maximum entries per layer")
	if flags.Parse(args[1:]) != nil || flags.NArg() != 0 {
		return 2
	}
	if action != "verify" && action != "extract" && action != "apply" && action != "status" && action != "review" {
		return 2
	}
	if (action == "verify" || action == "extract" || action == "apply") && *archive == "" || action == "extract" && *destination == "" || action == "apply" && (!*confirm || *database == "") || action == "review" && (!*confirm || *epoch == "" || *inventory == "" || *actor == "" || *reason == "") {
		fmt.Fprintln(stderr, "recovery.invalid_arguments")
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var a *recovery.Archive
	var err error
	if action == "verify" || action == "extract" || action == "apply" {
		a, err = recovery.Prepare(*archive, recovery.Limits{Bytes: *maxBytes, Entries: *maxEntries})
		if err != nil {
			fmt.Fprintln(stderr, "recovery.invalid_archive")
			return 1
		}
		defer a.Close()
	}
	var result any
	if action == "verify" {
		result = map[string]any{"status": "verified", "archive_sha256": a.Digest, "format": a.Format}
	}
	if action == "extract" {
		err = a.Extract(*destination)
		result = map[string]any{"status": "extracted", "archive_sha256": a.Digest}
	}
	if action == "apply" || action == "status" || action == "review" {
		pool, e := recovery.OpenPool(ctx, os.Getenv("DATABASE_DSN"))
		if e != nil {
			fmt.Fprintln(stderr, "recovery.database_unavailable")
			return 1
		}
		defer pool.Close()
		switch action {
		case "apply":
			result, err = recovery.Apply(ctx, pool, a, recovery.ApplyOptions{DataRoot: *root, Database: *database, Migrations: *migrations})
		case "status":
			result, err = recovery.Snapshot(ctx, pool)
		case "review":
			result, err = recovery.Review(ctx, pool, *root, *epoch, *inventory, *actor, *reason)
		}
	}
	if err != nil {
		fmt.Fprintln(stderr, "recovery.operation_failed; keep traffic offline and inspect local target state")
		return 1
	}
	if e := json.NewEncoder(stdout).Encode(result); e != nil {
		return 1
	}
	return 0
}
