package setup

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"
)

// When ITCH_SETUP_TEST_SLEEP is set, the test binary becomes an inert
// child process for the tests below: portable across platforms, unlike
// shelling out to a sleep command.
func TestMain(m *testing.M) {
	if d := os.Getenv("ITCH_SETUP_TEST_SLEEP"); d != "" {
		dur, err := time.ParseDuration(d)
		if err != nil {
			os.Exit(1)
		}
		time.Sleep(dur)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func startSleeperProcess(t *testing.T, d time.Duration) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("could not find test binary: %v", err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "ITCH_SETUP_TEST_SLEEP="+d.String())
	if err := cmd.Start(); err != nil {
		t.Fatalf("could not start sleeper process: %v", err)
	}
	return cmd
}

func Test_WaitForProcessToExit_ReturnsWhenProcessExits(t *testing.T) {
	cmd := startSleeperProcess(t, 300*time.Millisecond)
	go cmd.Wait() // reap so the zombie doesn't linger

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := WaitForProcessToExit(ctx, cmd.Process.Pid)
	if err != nil {
		t.Fatalf("expected nil after process exit, got: %v", err)
	}
}

func Test_WaitForProcessToExit_EnforcesTimeout(t *testing.T) {
	// a process that outlives the context
	cmd := startSleeperProcess(t, 30*time.Second)
	defer func() {
		cmd.Process.Kill()
		cmd.Wait()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- WaitForProcessToExit(ctx, cmd.Process.Pid)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a timeout error, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("WaitForProcessToExit did not return after context timeout")
	}
}
