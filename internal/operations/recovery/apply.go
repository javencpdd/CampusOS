package recovery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func CheckMigrations(ctx context.Context, pool *pgxpool.Pool, directory string) error {
	rows, e := pool.Query(ctx, `SELECT version,name,checksum FROM schema_migrations ORDER BY version`)
	if e != nil {
		return e
	}
	defer rows.Close()
	known := map[string]bool{}
	for rows.Next() {
		var version, name, checksum string
		if e = rows.Scan(&version, &name, &checksum); e != nil {
			return e
		}
		filename := name + ".up.sql"
		if !strings.HasPrefix(name, version+"_") {
			return errors.New("recovery.migration_mismatch")
		}
		actual, e := hashFile(filepath.Join(directory, filename))
		if e != nil || actual != checksum {
			return errors.New("recovery.migration_mismatch")
		}
		known[filename] = true
	}
	if e = rows.Err(); e != nil {
		return e
	}
	files, e := filepath.Glob(filepath.Join(directory, "*.up.sql"))
	if e != nil {
		return e
	}
	if len(files) != len(known) {
		return errors.New("recovery.migration_mismatch")
	}
	return nil
}

func Snapshot(ctx context.Context, pool *pgxpool.Pool) (map[string]any, error) {
	tx, e := pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	s, e := readState(ctx, tx)
	if e != nil || s == nil {
		return nil, ErrQuarantined
	}
	facts, sha, e := inventory(ctx, tx)
	if e != nil {
		return nil, e
	}
	return map[string]any{"state": s, "inventory_sha256": sha, "facts": facts, "authorization_reviewed": s.Phase == "reviewed", "egress_paused": s.EgressPaused}, nil
}

type ApplyOptions struct{ DataRoot, Database, Migrations string }

func Apply(ctx context.Context, pool *pgxpool.Pool, a *Archive, options ApplyOptions) (*State, error) {
	root, e := filepath.Abs(options.DataRoot)
	if e != nil || root == "/" || options.Database == "" || options.Migrations == "" {
		return nil, ErrQuarantined
	}
	var actualDB string
	if e = pool.QueryRow(ctx, `SELECT current_database()`).Scan(&actualDB); e != nil || actualDB != options.Database {
		return nil, errors.New("recovery.database_mismatch")
	}
	if e = CheckMigrations(ctx, pool, options.Migrations); e != nil {
		// A failed SQL restore may leave a partial schema. Only the same fully
		// verified archive may resume under an existing quarantined marker.
		marker, markerErr := ReadMarker(root)
		if markerErr != nil || marker == nil || marker.Phase != "quarantined" || marker.ArchiveSHA != a.Digest {
			return nil, e
		}
	}
	// Same runtime schema only; restoration never installs migration code from
	// the archive or downgrades the prebuilt host.
	current, e := filepath.Glob(filepath.Join(options.Migrations, "*.sql"))
	if e != nil {
		return nil, e
	}
	for _, file := range current {
		want, e := hashFile(file)
		if e != nil {
			return nil, e
		}
		got, e := hashFile(filepath.Join(a.Directory, "files/migrations", filepath.Base(file)))
		if e != nil || got != want {
			return nil, errors.New("recovery.archive_schema_mismatch")
		}
	}
	c, unlock, e := exclusive(ctx, pool)
	if e != nil {
		return nil, e
	}
	defer unlock()
	// No other database clients may be writing during the filesystem + SQL
	// replacement. The maintenance lock prevents normal Server startup races.
	var others int
	e = c.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND application_name<>'campusos-recovery'`).Scan(&others)
	if e != nil || others != 0 {
		return nil, errors.New("recovery.database_clients_active")
	}
	epoch, e := NewEpoch()
	if e != nil {
		return nil, e
	}
	s := &State{Epoch: epoch, ArchiveSHA: a.Digest, Phase: "quarantined", EgressPaused: true}
	if e = WriteMarker(root, *s); e != nil {
		return nil, e
	}
	// Every subsequent failure intentionally leaves the durable barrier in
	// place. Retry reapplies the same archive with a fresh authentication epoch.
	for _, name := range []string{"plugins", "plugin_data", "module_data", "resources", "personal-space", "images", "config", "skills"} {
		dest := filepath.Join(root, name)
		source := filepath.Join(a.Directory, "files/data", name)
		if e = os.RemoveAll(dest); e != nil {
			return nil, e
		}
		if info, err := os.Stat(source); err == nil {
			if !info.IsDir() {
				return nil, ErrArchive
			}
			if e = copyTree(source, dest); e != nil {
				return nil, e
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	cfg := pool.Config().ConnConfig
	command := exec.CommandContext(ctx, "psql", "-X", "-v", "ON_ERROR_STOP=1", "-f", filepath.Join(a.Directory, "database.sql"))
	// pgx retains TLS configuration separately, so preserve the operator's
	// explicit libpq SSL mode instead of inventing an empty environment value.
	command.Env = append(os.Environ(), "PGHOST="+cfg.Host, "PGPORT="+stringPort(cfg.Port), "PGDATABASE="+options.Database, "PGUSER="+cfg.User, "PGPASSWORD="+cfg.Password)
	if parsed, e := url.Parse(os.Getenv("DATABASE_DSN")); e == nil && (parsed.Scheme == "postgres" || parsed.Scheme == "postgresql") {
		for key, variable := range map[string]string{"sslmode": "PGSSLMODE", "sslrootcert": "PGSSLROOTCERT", "sslcert": "PGSSLCERT", "sslkey": "PGSSLKEY"} {
			if value := parsed.Query().Get(key); value != "" {
				command.Env = append(command.Env, variable+"="+value)
			}
		}
	}
	// SQL and stderr remain private. The public CLI emits stable failure codes,
	// never a connection string, a credential or restored private SQL contents.
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if e = command.Run(); e != nil {
		return nil, errors.New("recovery.sql_restore_failed")
	}
	if e = CheckMigrations(ctx, pool, options.Migrations); e != nil {
		return nil, e
	}
	if e = Quarantine(ctx, c, *s); e != nil {
		return nil, e
	}
	return s, nil
}

func stringPort(port uint16) string {
	const digits = "0123456789"
	if port == 0 {
		return "0"
	}
	var b [5]byte
	i := len(b)
	for port > 0 {
		i--
		b[i] = digits[port%10]
		port /= 10
	}
	return string(b[i:])
}

func OpenPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		return nil, ErrQuarantined
	}
	if strings.TrimSpace(cfg.ConnConfig.Database) == "" {
		return nil, ErrQuarantined
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "campusos-recovery"
	return pgxpool.NewWithConfig(ctx, cfg)
}

func SafeJSON(value any) ([]byte, error) { return json.MarshalIndent(value, "", "  ") }
