package container

import (
	"context"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Only the native platform resolver supplies these settings. Manifest fields,
// environment variables, PATH and Docker contexts are never executable inputs.
type dockerPlatform struct {
	os, binary, host, systemRoot string
}

type windowsSystemFolders func() (programFiles, systemRoot string, err error)

func dockerPlatformFor(goos string, folders windowsSystemFolders) (dockerPlatform, error) {
	switch goos {
	case "linux":
		return dockerPlatform{os: goos, binary: "/usr/bin/docker", host: "unix:///var/run/docker.sock"}, nil
	case "windows":
		if folders == nil {
			return dockerPlatform{}, ErrImagePlatform
		}
		programFiles, systemRoot, err := folders()
		if err != nil || !localWindowsSystemDirectory(programFiles) || !localWindowsSystemDirectory(systemRoot) {
			return dockerPlatform{}, ErrImagePlatform
		}
		return dockerPlatform{os: goos, binary: programFiles + `\Docker\Docker\resources\bin\docker.exe`, host: "npipe:////./pipe/docker_engine", systemRoot: systemRoot}, nil
	default:
		return dockerPlatform{}, ErrImagePlatform
	}
}

// System APIs provide canonical drive-absolute directories. Reject network,
// device, relative and environment-expanded forms instead of normalizing them.
func localWindowsSystemDirectory(path string) bool {
	if len(path) < 4 || len(path) > 512 || !((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) || path[1:3] != `:\` || strings.ContainsAny(path, "/\x00\r\n") {
		return false
	}
	for _, part := range strings.Split(path[3:], `\`) {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, `:"<>|?*%`) || strings.TrimRight(part, " .") != part {
			return false
		}
		for _, ch := range part {
			if ch < 0x20 {
				return false
			}
		}
	}
	return true
}

func localCommand(ctx context.Context, home string, args ...string) *exec.Cmd {
	if err := ctx.Err(); err != nil {
		cmd := exec.CommandContext(ctx, "")
		cmd.Err = err
		cmd.Env = []string{}
		return cmd
	}
	platform, err := currentDockerPlatform()
	return localCommandForPlatform(ctx, platform, err, home, args...)
}

func localCommandForPlatform(ctx context.Context, platform dockerPlatform, platformErr error, home string, args ...string) *exec.Cmd {
	if platformErr != nil || !filepath.IsAbs(home) || strings.ContainsAny(home, "\x00\r\n") || platform.os != "linux" && platform.os != "windows" {
		// exec.Cmd.Err prevents Start before any process or IO. Do not fall back
		// to an inherited PATH or C: when the trusted OS lookup is unavailable.
		cmd := exec.CommandContext(ctx, "")
		cmd.Err = ErrImagePlatform
		cmd.Env = []string{}
		return cmd
	}
	cmd := exec.CommandContext(ctx, platform.binary, append([]string{"--host=" + platform.host, "--config=" + home}, args...)...)
	if platform.os == "windows" {
		cmd.Env = []string{"SystemRoot=" + platform.systemRoot, "USERPROFILE=" + home, "DOCKER_CONFIG=" + home}
	} else {
		cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + home, "DOCKER_CONFIG=" + home}
	}
	cmd.WaitDelay = time.Second
	cmd.Stderr = io.Discard
	return cmd
}
