package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	storage "github.com/campusos/CampusOS/internal/modules/core/userstorage"
	schedule "github.com/campusos/CampusOS/internal/modules/features/schedule"
	"github.com/campusos/CampusOS/pkg/config"
	"github.com/campusos/CampusOS/pkg/idgen"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func runSchedule(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printScheduleUsage(stderr)
		return 2
	}
	switch args[0] {
	case "adopt":
		if err := runScheduleAdopt(args[1:], stdout); err != nil {
			fmt.Fprintf(stderr, "schedule adopt: %v\n", err)
			return 1
		}
		return 0
	case "help", "-h", "--help":
		printScheduleUsage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown schedule command: %s\n", args[0])
		printScheduleUsage(stderr)
		return 2
	}
}

func printScheduleUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: campusosctl schedule adopt [--root path] [--dsn url] [--dry-run] [--apply --actor operator --reason reason]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "The default scan is read-only. --apply registers validated legacy JSON as immutable Object copies, creates closed historical terms only when absent, maps valid index.json preferences, and writes an audit record.")
}

func runScheduleAdopt(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("schedule adopt", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", storage.DefaultRoot, "personal-space root")
	dsn := flags.String("dsn", "", "PostgreSQL DSN; defaults to DATABASE_DSN")
	dryRun := flags.Bool("dry-run", true, "report only (default)")
	apply := flags.Bool("apply", false, "adopt validated schedules")
	actor := flags.String("actor", "", "local authorized operator identity required with --apply")
	reason := flags.String("reason", "", "operator reason required with --apply, up to 500 characters")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("usage: campusosctl schedule adopt [--root path] [--dsn url] [--dry-run] [--apply --actor operator --reason reason]")
	}
	if *apply {
		*dryRun = false
		if strings.TrimSpace(*actor) == "" || strings.TrimSpace(*reason) == "" || len([]rune(strings.TrimSpace(*reason))) > 500 {
			return errors.New("--apply requires a non-empty --actor and a --reason of at most 500 characters")
		}
	}
	report, err := schedule.ScanHistoricalSchedules(*root, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("scan historical schedules: %w", err)
	}
	result := scheduleAdoptionResult{DryRun: *dryRun, Report: report}
	if *apply {
		configuredDSN := strings.TrimSpace(*dsn)
		if configuredDSN == "" {
			configuredDSN = config.Load().Database.DSN
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		pool, openErr := pgxpool.New(ctx, configuredDSN)
		if openErr != nil {
			return errors.New("connect schedule database")
		}
		defer pool.Close()
		if openErr = pool.Ping(ctx); openErr != nil {
			return errors.New("connect schedule database")
		}
		result.Actions, err = applyHistoricalSchedules(ctx, pool, report, strings.TrimSpace(*actor), strings.TrimSpace(*reason))
		if err != nil {
			return err
		}
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

type scheduleAdoptionResult struct {
	DryRun  bool                              `json:"dry_run"`
	Report  schedule.HistoricalScheduleReport `json:"report"`
	Actions map[string]int                    `json:"actions,omitempty"`
}

func applyHistoricalSchedules(ctx context.Context, pool *pgxpool.Pool, report schedule.HistoricalScheduleReport, actor, reason string) (map[string]int, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	actions := map[string]int{}
	createdFiles := make([]string, 0)
	cleanupFiles := func() {
		for _, path := range createdFiles {
			_ = os.Remove(path)
		}
	}
	for _, item := range report.Candidates {
		termID, termCreated, err := ensureHistoricalTerm(ctx, tx, item)
		if err != nil {
			cleanupFiles()
			return nil, fmt.Errorf("ensure historical term %d/%s: %w", item.TermYear, item.Semester, err)
		}
		if termCreated {
			actions["academic_terms_created"]++
		}
		objectID, objectCreated, objectPath, err := ensureHistoricalScheduleObject(ctx, tx, report.Root, item)
		if err != nil {
			cleanupFiles()
			return nil, fmt.Errorf("adopt %s: %w", item.SourcePath, err)
		}
		if objectCreated {
			createdFiles = append(createdFiles, objectPath)
			actions["objects_created"]++
		}
		if _, err = tx.Exec(ctx, `INSERT INTO user_schedule_terms (user_id,academic_term_id,current_object_id,first_week_start,version,created_at,updated_at)
			VALUES ($1::bigint,$2::bigint,$3::bigint,$4::date,1,NOW(),NOW())
			ON CONFLICT (user_id,academic_term_id) DO UPDATE SET
				current_object_id=EXCLUDED.current_object_id, first_week_start=EXCLUDED.first_week_start,
				version=CASE WHEN user_schedule_terms.current_object_id IS DISTINCT FROM EXCLUDED.current_object_id THEN user_schedule_terms.version+1 ELSE user_schedule_terms.version END,
				updated_at=NOW()`, item.OwnerID, termID, objectID, item.FirstWeekStart); err != nil {
			cleanupFiles()
			return nil, err
		}
		actions["schedule_bindings_upserted"]++
		if item.ActiveInIndex {
			if _, err = tx.Exec(ctx, `INSERT INTO user_schedule_preferences (user_id,academic_term_id,updated_at)
				VALUES ($1::bigint,$2::bigint,NOW()) ON CONFLICT (user_id) DO UPDATE SET academic_term_id=EXCLUDED.academic_term_id,updated_at=NOW()`, item.OwnerID, termID); err != nil {
				cleanupFiles()
				return nil, err
			}
			actions["preferences_upserted"]++
		}
	}
	details, err := json.Marshal(map[string]any{"reason": reason, "actions": actions, "valid_candidates": len(report.Candidates), "issues": len(report.Issues)})
	if err != nil {
		cleanupFiles()
		return nil, err
	}
	auditID := strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
	if _, err = tx.Exec(ctx, `INSERT INTO platform_command_audits (id,command_id,command_code,actor_id,actor_type,resource_type,resource_id,operation_code,permission_code,details,created_at)
		VALUES ($1,$1,'local.schedule.adopt.apply',$2,'local-operator','schedule','historical-json','cli.schedule.adopt','schedule.adopt.apply',$3,NOW())`, auditID, actor, details); err != nil {
		cleanupFiles()
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		cleanupFiles()
		return nil, err
	}
	return actions, nil
}

func ensureHistoricalTerm(ctx context.Context, tx pgx.Tx, item schedule.HistoricalSchedule) (id string, created bool, err error) {
	if err = tx.QueryRow(ctx, `SELECT id::text FROM academic_terms WHERE year=$1 AND semester=$2`, item.TermYear, item.Semester).Scan(&id); err == nil {
		return id, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	id = strconv.FormatInt(idgen.New(), 10)
	err = tx.QueryRow(ctx, `INSERT INTO academic_terms (id,year,semester,first_week_start,status,is_default,version,created_at,updated_at,closed_at)
		VALUES ($1::bigint,$2,$3,$4::date,'closed',FALSE,1,NOW(),NOW(),NOW()) RETURNING id::text`, id, item.TermYear, item.Semester, item.FirstWeekStart).Scan(&id)
	return id, err == nil, err
}

func ensureHistoricalScheduleObject(ctx context.Context, tx pgx.Tx, root string, item schedule.HistoricalSchedule) (id string, created bool, path string, err error) {
	err = tx.QueryRow(ctx, `SELECT id::text FROM storage_objects WHERE owner_user_id=$1::bigint AND namespace='schedule' AND purpose='historical-term-json' AND sha256=$2 AND status='ready' ORDER BY created_at ASC LIMIT 1`, item.OwnerID, item.SHA256).Scan(&id)
	if err == nil {
		return id, false, "", nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", false, "", err
	}
	id = strconv.FormatInt(idgen.New(), 10)
	key := id + ".bin"
	path, err = legacyObjectPath(root, item.OwnerID, key)
	if err != nil {
		return "", false, "", err
	}
	source, err := sourceUnderRoot(root, item.SourcePath)
	if err != nil {
		return "", false, "", err
	}
	if err = copyCheckedFile(source, path, item.SHA256, item.SizeBytes); err != nil {
		return "", false, "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO storage_objects (id,owner_user_id,namespace,purpose,provider,storage_key,original_name,mime_type,size_bytes,sha256,status,version,created_at,updated_at)
		VALUES ($1::bigint,$2::bigint,'schedule','historical-term-json','local',$3,$4,'application/json',$5,$6,'ready',1,NOW(),NOW())`, id, item.OwnerID, key, filepath.Base(item.SourcePath), item.SizeBytes, item.SHA256); err != nil {
		_ = os.Remove(path)
		return "", false, "", err
	}
	usage, err := userPhysicalUsage(root, item.OwnerID)
	if err != nil {
		_ = os.Remove(path)
		return "", false, "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO user_storage_accounts (user_id,used_bytes,reserved_bytes,version,created_at,updated_at)
		VALUES ($1::bigint,$2,0,1,NOW(),NOW())
		ON CONFLICT (user_id) DO UPDATE SET used_bytes=EXCLUDED.used_bytes,version=user_storage_accounts.version+1,updated_at=NOW()`, item.OwnerID, usage); err != nil {
		_ = os.Remove(path)
		return "", false, "", err
	}
	return id, true, path, nil
}

func legacyObjectPath(root, owner, key string) (string, error) {
	if !storage.SafeSegment(owner) || !storage.SafeSegment(key) {
		return "", errors.New("unsafe storage object path")
	}
	base, err := filepath.Abs(filepath.Join(root, owner, storage.FileDir, "objects"))
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(base, key))
	if err != nil || !strings.HasPrefix(path, base+string(os.PathSeparator)) {
		return "", errors.New("unsafe storage object path")
	}
	return path, nil
}

func sourceUnderRoot(root, relative string) (string, error) {
	base, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	path, err := filepath.Abs(filepath.Join(base, filepath.FromSlash(relative)))
	if err != nil || !strings.HasPrefix(path, base+string(os.PathSeparator)) {
		return "", errors.New("source path escapes personal-space root")
	}
	return path, nil
}

func copyCheckedFile(source, target, expectedHash string, expectedSize int64) error {
	sourceFile, err := os.Open(source)
	if err != nil {
		return err
	}
	defer sourceFile.Close()
	if err = os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(target), ".adopt-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() { _ = temp.Close(); _ = os.Remove(tempPath) }()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temp, hash), sourceFile)
	if err != nil {
		return err
	}
	if written != expectedSize || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expectedHash) {
		return errors.New("source file changed after dry-run; rerun the adoption scan")
	}
	if err = temp.Sync(); err != nil {
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, target)
}

func userPhysicalUsage(root, owner string) (int64, error) {
	base, err := filepath.Abs(filepath.Join(root, owner))
	if err != nil {
		return 0, err
	}
	var total int64
	err = filepath.WalkDir(base, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return errors.New("symbolic link in personal-space")
		}
		if entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		total += info.Size()
		return nil
	})
	return total, err
}
