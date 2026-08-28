package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	storage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func runStorage(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printStorageUsage(stderr)
		return 2
	}
	switch args[0] {
	case "reconcile":
		if err := runStorageReconcile(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "storage reconcile: %v\n", err)
			return 1
		}
		return 0
	case "help", "-h", "--help":
		printStorageUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown storage command: %s\n", args[0])
		printStorageUsage(stderr)
		return 2
	}
}

func printStorageUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: campusosctl storage reconcile [--root path] [--dsn url] [--dry-run] [--apply --actor operator --reason reason]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "The default is read-only. --apply only expires stale reservations and marks ready metadata whose provider file is absent as missing; unknown files and ledger mismatches are never changed automatically.")
}

func runStorageReconcile(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("storage reconcile", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", storage.DefaultRoot, "personal-space root")
	dsn := flags.String("dsn", "", "PostgreSQL DSN; defaults to DATABASE_DSN")
	dryRun := flags.Bool("dry-run", true, "report only (default)")
	apply := flags.Bool("apply", false, "apply the narrow safe repair set")
	actor := flags.String("actor", "", "local authorized operator identity required with --apply")
	reason := flags.String("reason", "", "operator reason required with --apply, up to 500 characters")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("usage: campusosctl storage reconcile [--root path] [--dsn url] [--dry-run] [--apply --actor operator --reason reason]")
	}
	if *apply {
		*dryRun = false
		if strings.TrimSpace(*actor) == "" || strings.TrimSpace(*reason) == "" || len([]rune(strings.TrimSpace(*reason))) > 500 {
			return errors.New("--apply requires a non-empty --actor and a --reason of at most 500 characters")
		}
	}
	configuredDSN := strings.TrimSpace(*dsn)
	if configuredDSN == "" {
		configuredDSN = config.Load().Database.DSN
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, configuredDSN)
	if err != nil {
		return errors.New("connect storage database")
	}
	defer pool.Close()
	if err = pool.Ping(ctx); err != nil {
		return errors.New("connect storage database")
	}
	snapshot, err := loadStorageSnapshot(ctx, pool)
	if err != nil {
		return fmt.Errorf("read storage metadata: %w", err)
	}
	report, err := storage.ReconcileLocal(*root, snapshot, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("scan local provider: %w", err)
	}
	result := storageReconcileResult{DryRun: *dryRun, Report: report}
	if *apply {
		result.Actions, err = applyStorageReconcile(ctx, pool, report, strings.TrimSpace(*actor), strings.TrimSpace(*reason))
		if err != nil {
			return err
		}
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

type storageReconcileResult struct {
	DryRun  bool                    `json:"dry_run"`
	Report  storage.ReconcileReport `json:"report"`
	Actions map[string]int          `json:"actions,omitempty"`
}

func loadStorageSnapshot(ctx context.Context, pool *pgxpool.Pool) (storage.ReconcileSnapshot, error) {
	snapshot := storage.ReconcileSnapshot{}
	rows, err := pool.Query(ctx, `SELECT id::text,owner_user_id::text,storage_key,size_bytes,sha256,status,updated_at FROM storage_objects`)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var item storage.ReconcileObject
		if err = rows.Scan(&item.ID, &item.OwnerID, &item.StorageKey, &item.SizeBytes, &item.SHA256, &item.Status, &item.UpdatedAt); err != nil {
			rows.Close()
			return snapshot, err
		}
		snapshot.Objects = append(snapshot.Objects, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return snapshot, err
	}
	rows.Close()
	rows, err = pool.Query(ctx, `SELECT id::text,object_id::text,user_id::text,status,expires_at FROM user_storage_reservations`)
	if err != nil {
		return snapshot, err
	}
	for rows.Next() {
		var item storage.ReconcileReservation
		if err = rows.Scan(&item.ID, &item.ObjectID, &item.OwnerID, &item.Status, &item.ExpiresAt); err != nil {
			rows.Close()
			return snapshot, err
		}
		snapshot.Reservations = append(snapshot.Reservations, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return snapshot, err
	}
	rows.Close()
	rows, err = pool.Query(ctx, `SELECT user_id::text,used_bytes,reserved_bytes FROM user_storage_accounts`)
	if err != nil {
		return snapshot, err
	}
	defer rows.Close()
	for rows.Next() {
		var item storage.ReconcileAccount
		if err = rows.Scan(&item.OwnerID, &item.UsedBytes, &item.ReservedBytes); err != nil {
			return snapshot, err
		}
		snapshot.Accounts = append(snapshot.Accounts, item)
	}
	return snapshot, rows.Err()
}

func applyStorageReconcile(ctx context.Context, pool *pgxpool.Pool, report storage.ReconcileReport, actor, reason string) (map[string]int, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	actions := map[string]int{}
	seenReservations := map[string]struct{}{}
	seenObjects := map[string]struct{}{}
	for _, item := range report.Differences {
		switch item.Kind {
		case storage.ReconcileReservationExpired:
			if _, duplicate := seenReservations[item.ReservationID]; duplicate || item.ReservationID == "" {
				continue
			}
			seenReservations[item.ReservationID] = struct{}{}
			var owner, objectID string
			var bytes int64
			err = tx.QueryRow(ctx, `UPDATE user_storage_reservations SET status='expired',updated_at=NOW()
				WHERE id=$1::bigint AND status='pending' AND expires_at<=NOW()
				RETURNING user_id::text,object_id::text,reserved_bytes`, item.ReservationID).Scan(&owner, &objectID, &bytes)
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, `UPDATE user_storage_accounts SET reserved_bytes=GREATEST(0,reserved_bytes-$2),version=version+1,updated_at=NOW() WHERE user_id=$1::bigint`, owner, bytes); err != nil {
				return nil, err
			}
			if _, err = tx.Exec(ctx, `UPDATE storage_objects SET status='deleted',deleted_at=NOW(),version=version+1,updated_at=NOW() WHERE id=$1::bigint AND status='pending'`, objectID); err != nil {
				return nil, err
			}
			actions[storage.ReconcileReservationExpired]++
		case storage.ReconcileMetadataMissingFile:
			if _, duplicate := seenObjects[item.ObjectID]; duplicate || item.ObjectID == "" {
				continue
			}
			seenObjects[item.ObjectID] = struct{}{}
			command, updateErr := tx.Exec(ctx, `UPDATE storage_objects SET status='missing',version=version+1,updated_at=NOW() WHERE id=$1::bigint AND status='ready'`, item.ObjectID)
			if updateErr != nil {
				return nil, updateErr
			}
			if command.RowsAffected() == 1 {
				actions[storage.ReconcileMetadataMissingFile]++
			}
		}
	}
	details, err := json.Marshal(map[string]any{"reason": reason, "actions": actions, "difference_counts": report.Counts})
	if err != nil {
		return nil, err
	}
	auditID := strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	if _, err = tx.Exec(ctx, `INSERT INTO platform_command_audits (id,command_id,command_code,actor_id,actor_type,resource_type,resource_id,operation_code,permission_code,details,created_at)
		VALUES ($1,$1,'local.storage.reconcile.apply',$2,'local-operator','user_storage','local-provider','cli.storage.reconcile','storage.reconcile.apply',$3,NOW())`, auditID, actor, details); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return actions, nil
}
