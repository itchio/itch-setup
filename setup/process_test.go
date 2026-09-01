package setup

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func Test_WaitForProcessToExit_ReturnsWhenProcessExits(t *testing.T) {
	cmd := exec.Command("sleep", "0.3")
	if err := cmd.Start(); err != nil {
		t.Fatalf("could not start sleep: %v", err)
	}
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
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("could not start sleep: %v", err)
	}
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
