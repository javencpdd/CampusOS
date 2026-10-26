package container

import (
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestV5ContainerLocalPlatformSystemPaths(t *testing.T) {
	// The injected folder reader is a contract oracle, not Windows acceptance.
	p, err := dockerPlatformFor("windows", func() (string, string, error) { return `D:\Program Files`, `E:\Windows`, nil })
	if err != nil || p.binary != `D:\Program Files\Docker\Docker\resources\bin\docker.exe` || p.systemRoot != `E:\Windows` || p.host != "npipe:////./pipe/docker_engine" {
		t.Fatal("non-C system directories lost", p, err)
	}
	p, err = dockerPlatformFor("windows", func() (string, string, error) { return `D:\程序`, `E:\系统`, nil })
	if err != nil || !strings.HasPrefix(p.binary, `D:\程序\`) {
		t.Fatal("Unicode system directory", err)
	}
	p, err = dockerPlatformFor("linux", func() (string, string, error) { t.Fatal("Linux consulted Windows folders"); return "", "", nil })
	if err != nil || p.binary != "/usr/bin/docker" || p.host != "unix:///var/run/docker.sock" || p.systemRoot != "" {
		t.Fatal(p, err)
	}
}

func TestV5ContainerLocalPlatformRejectsUnsafeRoots(t *testing.T) {
	for _, bad := range []string{"", `C:relative`, `\Windows`, `\\server\share`, `\\?\C:\Windows`, `C:/Windows`, `C:\Windows\..\other`, `C:\Windows\.`, `C:\Windows\\dir`, `C:\Windows\`, `C:\Windows.`, `C:\Windows `, `C:\Windows:stream`, `%ProgramFiles%`, "C:\\Windows\n", "C:\\Win\x00dows", "C:\\Win\tdows"} {
		t.Run(strings.ReplaceAll(bad, "\x00", "NUL"), func(t *testing.T) {
			for _, system := range []bool{false, true} {
				_, err := dockerPlatformFor("windows", func() (string, string, error) {
					if system {
						return `C:\Program Files`, bad, nil
					}
					return bad, `C:\Windows`, nil
				})
				if err != ErrImagePlatform {
					t.Fatal("unsafe system root admitted", bad, err)
				}
			}
		})
	}
	for _, goos := range []string{"darwin", "", "plan9"} {
		if _, e := dockerPlatformFor(goos, nil); e != ErrImagePlatform {
			t.Fatal("unsupported platform", e)
		}
	}
	if _, e := dockerPlatformFor("windows", nil); e != ErrImagePlatform {
		t.Fatal("absent OS reader")
	}
	if _, e := dockerPlatformFor("windows", func() (string, string, error) {
		return `C:\Program Files`, `C:\Windows`, errors.New("private OS detail")
	}); e != ErrImagePlatform {
		t.Fatal("OS lookup failure did not close", e)
	}
}

func TestV5ContainerLocalCommandEnvironmentAndFailure(t *testing.T) {
	for _, key := range []string{"DOCKER_HOST", "DOCKER_CONTEXT", "DOCKER_CONFIG", "HTTP_PROXY", "DATABASE_DSN", "ProgramFiles", "SystemRoot", "PATHEXT"} {
		t.Setenv(key, "HOST_SENTINEL_MUST_NOT_BE_USED")
	}
	home := t.TempDir()
	p, e := currentDockerPlatform()
	if e != nil {
		t.Fatal(e)
	}
	cmd := localCommand(context.Background(), home, "image", "inspect", "--", "signed-ref")
	if cmd.Path != p.binary || !reflect.DeepEqual(cmd.Args[1:], []string{"--host=" + p.host, "--config=" + home, "image", "inspect", "--", "signed-ref"}) || cmd.WaitDelay != time.Second || cmd.Stderr != io.Discard {
		t.Fatal("unscoped CLI command")
	}
	want := []string{"PATH=/usr/bin:/bin", "HOME=" + home, "DOCKER_CONFIG=" + home}
	if runtime.GOOS == "windows" {
		want = []string{"SystemRoot=" + p.systemRoot, "USERPROFILE=" + home, "DOCKER_CONFIG=" + home}
	}
	if !reflect.DeepEqual(cmd.Env, want) || strings.Contains(strings.Join(cmd.Env, "\n"), "HOST_SENTINEL") {
		t.Fatal("inherited host environment")
	}
	for _, badHome := range []string{"", "relative", "\x00", "/tmp/config\n"} {
		if e := localCommand(context.Background(), badHome, "info").Run(); e != ErrImagePlatform {
			t.Fatal("invalid CLI home ran", e)
		}
	}
	failed := localCommandForPlatform(context.Background(), dockerPlatform{}, errors.New("private OS failure"), home, "info")
	if e := failed.Run(); e != ErrImagePlatform || failed.Process != nil || len(failed.Env) != 0 {
		t.Fatal("OS failure spawned fallback", e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if cmd := localCommand(ctx, home, "info"); cmd.Run() != context.Canceled || cmd.Process != nil || len(cmd.Env) != 0 {
		t.Fatal("cancelled command resolved or started")
	}
	executable, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	p.binary = executable // Existing executable; cancellation must precede Start.
	if e := localCommandForPlatform(ctx, p, nil, home, "info").Run(); e != context.Canceled {
		t.Fatal("cancelled CLI started", e)
	}
}

func TestV5ContainerLocalWindowsEnvironmentContract(t *testing.T) {
	// Inspect construction only. No synthetic Windows process is executed.
	p, e := dockerPlatformFor("windows", func() (string, string, error) { return `D:\Program Files`, `E:\Windows`, nil })
	if e != nil {
		t.Fatal(e)
	}
	home := t.TempDir()
	cmd := localCommandForPlatform(context.Background(), p, nil, home, "info")
	if !reflect.DeepEqual(cmd.Env, []string{`SystemRoot=E:\Windows`, "USERPROFILE=" + home, "DOCKER_CONFIG=" + home}) || !reflect.DeepEqual(cmd.Args, []string{p.binary, "--host=npipe:////./pipe/docker_engine", "--config=" + home, "info"}) {
		t.Fatal("Windows local command contract")
	}
}
