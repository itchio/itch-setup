package test

import (
	"os/exec"
	"strconv"
	"testing"
	"time"

	"github.com/itchio/itch-setup/test/harness"
)

func TestRelaunch_WaitForProcess(t *testing.T) {
	h := harness.New(t)
	defer h.Cleanup()

	// Set up a version to relaunch
	mv := harness.NewMultiverseSetup(t, h.TempDir(), "itch")
	mv.CreateFullSetup("1.0.0")

	// Start a short-lived process (sleep for 2 seconds)
	sleepCmd := exec.Command("sleep", "2")
	if err := sleepCmd.Start(); err != nil {
		t.Fatalf("Failed to start sleep process: %v", err)
	}
	pid := sleepCmd.Process.Pid
	t.Logf("Started sleep process with PID %d", pid)

	// Reap the process in the background so it doesn't become a zombie.
	// itch-setup kills the process, but the zombie persists until the
	// parent (this test) calls Wait. Without this, ps.FindProcess still
	// sees the zombie and WaitForProcessToExit loops forever.
	go sleepCmd.Wait()

	// Run relaunch in a goroutine since it will block waiting
	resultChan := make(chan *harness.Result, 1)
	go func() {
		result := h.Run(
			"--appname", "itch",
			"--relaunch",
			"--relaunch-pid", strconv.Itoa(pid),
		)
		resultChan <- result
	}()

	// Wait for the result with a timeout
	select {
	case result := <-resultChan:
		t.Logf("Exit code: %d", result.ExitCode)
		t.Logf("Stdout:\n%s", result.Stdout)
		t.Logf("Stderr:\n%s", result.Stderr)
		t.Logf("Messages: %d", len(result.Messages))
		for i, msg := range result.Messages {
			t.Logf("  [%d] type=%s", i, msg.Type)
		}

		// Should emit ready-to-relaunch while waiting for the process
		if !result.HasMessageType(harness.TypeReadyToRelaunch) {
			t.Errorf("Expected ready-to-relaunch message, got messages: %v", result.Messages)
		}

	case <-time.After(30 * time.Second):
		t.Fatal("Test timed out waiting for relaunch to complete")
	}
}

func TestRelaunch_ProcessAlreadyExited(t *testing.T) {
	h := harness.New(t)
	defer h.Cleanup()

	// Set up a version to relaunch
	mv := harness.NewMultiverseSetup(t, h.TempDir(), "itch")
	mv.CreateFullSetup("1.0.0")

	// Start and immediately wait for a process to exit
	cmd := exec.Command("true") // exits immediately with code 0
	if err := cmd.Start(); err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}
	pid := cmd.Process.Pid
	cmd.Wait() // Wait for it to exit

	t.Logf("Process %d has already exited", pid)

	// Run relaunch - should handle already-exited process gracefully
	resultChan := make(chan *harness.Result, 1)
	go func() {
		result := h.Run(
			"--appname", "itch",
			"--relaunch",
			"--relaunch-pid", strconv.Itoa(pid),
		)
		resultChan <- result
	}()

	// Wait for the result with a timeout
	select {
	case result := <-resultChan:
		t.Logf("Exit code: %d", result.ExitCode)
		t.Logf("Stdout:\n%s", result.Stdout)
		t.Logf("Stderr:\n%s", result.Stderr)
		t.Logf("Messages: %d", len(result.Messages))
		for i, msg := range result.Messages {
			t.Logf("  [%d] type=%s", i, msg.Type)
		}

		// Process already exited, so should proceed to launch
		// The test will fail at launch (mock executable) but that's expected

	case <-time.After(30 * time.Second):
		t.Fatal("Test timed out")
	}
}

func TestRelaunch_InvalidPID(t *testing.T) {
	h := harness.New(t)
	defer h.Cleanup()

	// Set up a version
	mv := harness.NewMultiverseSetup(t, h.TempDir(), "itch")
	mv.CreateFullSetup("1.0.0")

	// Run relaunch with invalid PID (0)
	result := h.Run(
		"--appname", "itch",
		"--relaunch",
		"--relaunch-pid", "0",
	)

	t.Logf("Exit code: %d", result.ExitCode)
	t.Logf("Stderr:\n%s", result.Stderr)

	// Should fail with non-zero exit code
	if result.ExitCode == 0 {
		t.Errorf("Expected non-zero exit code for invalid PID")
	}
}
