package server

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/campusos/CampusOS/internal/projectaudit"
)

// This is an additional Linux08a target, not a reinterpretation of the04
// report. The runner must supply OS read-only mounts and an actual CLI-signed
// PDF archive. Missing isolation fails the required target rather than skips.
func TestPluginV5ReadOnlyHostActualServerBrowser(t *testing.T) {
	runV5ReadOnlyHostBrowser(t, false)
}

func TestPluginV5EcosystemReadOnlyHostBrowser(t *testing.T) {
	runV5ReadOnlyHostBrowser(t, true)
}

func runV5ReadOnlyHostBrowser(t *testing.T, ecosystem bool) {
	dsn := os.Getenv("CAMPUSOS_V5_READONLY_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires owned08a read-only production host target")
	}
	work, binary := os.Getenv("CAMPUSOS_V5_READONLY_OUTPUT"), os.Getenv("CAMPUSOS_V5_READONLY_SERVER_BINARY")
	if work == "" || binary == "" {
		t.Fatal("required read-only target inputs missing")
	}
	root, err := projectaudit.FindRepositoryRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{binary}
	for _, name := range []string{"go.mod", "go.sum", "cmd/server/main.go", "sdk/go/v5_container_chat.go", "web/pnpm-lock.yaml", "admin/pnpm-lock.yaml", "sdk/typescript/pnpm-lock.yaml", "web/dist/index.html", "admin/dist/index.html", "plugins/campusos.pdf-viewer/plugin.json"} {
		paths = append(paths, filepath.Join(root, name))
	}
	if err := v5RequireReadOnlyFiles(paths); err != nil {
		t.Fatal(err)
	}
	// Refuse merely protecting existing file modes: new source files must also
	// be impossible. No byte is written to an existing source file by any probe.
	probe := filepath.Join(root, fmt.Sprintf(".v12-08a-readonly-probe-%d", os.Getpid()))
	f, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if f != nil {
		_ = f.Close()
		_ = os.Remove(probe)
	}
	if !errors.Is(err, syscall.EROFS) {
		t.Fatal("source creation did not fail with EROFS", err)
	}
	database := "campusos_v12_08a_readonly_pdf"
	if ecosystem {
		database = "campusos_v12_08a_exit"
	}
	runPluginV5UIActualServerBrowser(t, v5UIActualTarget{
		dsn: dsn, binary: binary, work: filepath.Join(work, "browser"), database: database, ecosystem: ecosystem, tailOnly: ecosystem && os.Getenv("CAMPUSOS_V5_ECOSYSTEM_DIAGNOSTIC") == "true",
		pdfInput: filepath.Join(work, "input"),
		observe: func(t *testing.T, process *os.Process, processDir string) func() {
			before, err := v5ReadOnlyServerIdentity(process.Pid, binary)
			if err != nil {
				t.Fatal(err)
			}
			namespace, err := os.Readlink("/proc/self/ns/mnt")
			if err != nil || namespace != before.MountNamespace {
				t.Fatal("actual cmd/server did not inherit enforced mount namespace", err)
			}
			// Probe through the live server's root as well as this test process.
			// Linux returns ETXTBSY before EROFS for an executing ELF. Its
			// actual read-only mount is required by the identity check; all
			// other files must still return EROFS through the live server root.
			serverPaths := make([]string, len(paths)-1)
			for n, path := range paths[1:] {
				serverPaths[n] = fmt.Sprintf("/proc/%d/root%s", process.Pid, path)
			}
			if err := v5RequireReadOnlyFiles(serverPaths); err != nil {
				t.Fatal(err)
			}
			if f, err := os.CreateTemp(processDir, "allowed-runtime-data-"); err != nil {
				t.Fatal("owned runtime data must remain writable", err)
			} else {
				_ = f.Close()
				_ = os.Remove(f.Name())
			}
			return func() {
				after, err := v5ReadOnlyServerIdentity(process.Pid, binary)
				if err != nil || before != after {
					t.Fatal("production server restarted or its binary changed", err)
				}
				if err := v5RequireReadOnlyFiles(serverPaths); err != nil {
					t.Fatal(err)
				}
				if t.Failed() {
					return
				}
				proof := map[string]any{"status": "passed", "actual_cmd_server": !ecosystem, "actual_server_composition": true, "before": before, "after": after, "readonly_errno": "EROFS", "protected_files": paths, "live_root_protected_files": paths[1:], "new_source_file_rejected": true, "runtime_data_writable": true, "same_mount_namespace": true, "restart_count": 0, "headless_package_installed": ecosystem}
				wire, _ := json.MarshalIndent(proof, "", "  ")
				if err := os.WriteFile(filepath.Join(work, "readonly-host-proof.json"), append(wire, '\n'), 0600); err != nil {
					t.Fatal(err)
				}
			}
		},
	})
}

type v5ReadOnlyProcessIdentity struct {
	PID            int    `json:"pid"`
	StartTime      string `json:"start_time_ticks"`
	BinarySHA256   string `json:"binary_sha256"`
	MountNamespace string `json:"mount_namespace"`
	BinaryMountRO  bool   `json:"binary_mount_readonly"`
}

func v5ReadOnlyServerIdentity(pid int, binary string) (v5ReadOnlyProcessIdentity, error) {
	identity := v5ReadOnlyProcessIdentity{PID: pid}
	prefix := fmt.Sprintf("/proc/%d/", pid)
	stat, err := os.ReadFile(prefix + "stat")
	if err != nil {
		return identity, err
	}
	end := strings.LastIndex(string(stat), ") ")
	if end < 0 {
		return identity, errors.New("invalid process identity")
	}
	fields := strings.Fields(string(stat)[end+2:])
	if len(fields) <= 19 || fields[0] == "Z" {
		return identity, errors.New("server is absent or exited")
	}
	identity.StartTime = fields[19]
	if _, err = strconv.ParseUint(identity.StartTime, 10, 64); err != nil {
		return identity, err
	}
	exe, err := os.Readlink(prefix + "exe")
	if err != nil || exe != binary {
		return identity, errors.New("server is not the frozen cmd/server ELF")
	}
	raw, err := os.ReadFile(prefix + "exe")
	if err != nil {
		return identity, err
	}
	identity.BinarySHA256 = fmt.Sprintf("%x", sha256.Sum256(raw))
	identity.MountNamespace, err = os.Readlink(prefix + "ns/mnt")
	if err != nil {
		return identity, err
	}
	mounts, err := os.ReadFile(prefix + "mountinfo")
	if err != nil {
		return identity, err
	}
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	for _, line := range strings.Split(string(mounts), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 5 && unescape.Replace(fields[4]) == filepath.Dir(binary) {
			identity.BinaryMountRO = strings.Contains(","+fields[5]+",", ",ro,")
		}
	}
	if !identity.BinaryMountRO {
		return identity, errors.New("live server binary mount is not read-only")
	}
	return identity, nil
}

func v5RequireReadOnlyFiles(paths []string) error {
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("missing protected file %s: %w", path, err)
		}
		f, err := os.OpenFile(path, os.O_WRONLY, 0) // Never truncate or write.
		if f != nil {
			_ = f.Close()
		}
		if !errors.Is(err, syscall.EROFS) {
			return fmt.Errorf("protected file did not reject writes with EROFS: %s (%v)", path, err)
		}
	}
	return nil
}

func TestV5ReadOnlyHostRejectsFileModeOnlyEvidence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mode-only")
	if err := os.WriteFile(path, []byte("must remain unchanged"), 0444); err != nil {
		t.Fatal(err)
	}
	if err := v5RequireReadOnlyFiles([]string{path}); err == nil {
		t.Fatal("file mode or a writable mount was accepted as read-only isolation")
	}
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != "must remain unchanged" {
		t.Fatal("negative control changed file bytes", err)
	}
}
