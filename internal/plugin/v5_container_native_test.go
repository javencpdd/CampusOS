package plugin_test

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"testing"
	"time"
)

// Acceptance commands use a local daemon and an empty, test-owned CLI config.
// Inherited Docker contexts, credentials and PATH are not executable inputs.
func v5ContainerDocker(t *testing.T, ctx context.Context, args ...string) *exec.Cmd {
	t.Helper()
	home := t.TempDir()
	binary, host, environment := v5ContainerNativeEnvironment(t, home)
	cmd := exec.CommandContext(ctx, binary, append([]string{"--host=" + host, "--config=" + home}, args...)...)
	cmd.Env = environment
	cmd.WaitDelay = time.Second
	return cmd
}

// A real child verifies the native graceful signal before the Docker/PG drill.
// Forced termination cannot satisfy the clean-exit assertion.
func TestV5ContainerNativeShutdown(t *testing.T) {
	if os.Getenv("CAMPUSOS_V5_CONTAINER_SIGNAL_PROBE") == "true" {
		interrupt := make(chan os.Signal, 1)
		signal.Notify(interrupt, os.Interrupt, v5ContainerTerminationSignal())
		defer signal.Stop(interrupt)
		fmt.Println("native-signal-ready")
		select {
		case <-interrupt:
			return
		case <-time.After(10 * time.Second):
			t.Fatal("native graceful signal not received")
		}
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-test.run=^TestV5ContainerNativeShutdown$", "-test.timeout=12s")
	_, _, command.Env = v5ContainerNativeEnvironment(t, t.TempDir())
	command.Env = append(command.Env, "CAMPUSOS_V5_CONTAINER_SIGNAL_PROBE=true")
	command.Stderr = os.Stderr
	v5ContainerPrepareServer(command)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer command.Process.Kill()
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "native-signal-ready\n" {
		t.Fatal("native child readiness", line, err)
	}
	if err := v5ContainerStopServer(command); err != nil {
		t.Fatal("native graceful signal", err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal("native child clean exit", err)
	}
}
