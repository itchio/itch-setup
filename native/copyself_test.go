package native

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// A direct write to an executing binary fails with ETXTBSY on Linux; the
// temp-and-rename swap must succeed and leave the old process running.
func TestCopySelfOverRunningBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the running-exe swap works differently on Windows")
	}

	dir := t.TempDir()
	victimPath := filepath.Join(dir, "victim")

	sleepPath, err := exec.LookPath("sleep")
	if err != nil {
		t.Skipf("no sleep binary: %v", err)
	}
	sleepBytes, err := os.ReadFile(sleepPath)
	if err != nil {
		t.Fatalf("reading sleep: %v", err)
	}
	if err := os.WriteFile(victimPath, sleepBytes, 0o755); err != nil {
		t.Fatalf("installing victim: %v", err)
	}

	cmd := exec.Command(victimPath, "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting victim: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	if _, err := CopySelf(victimPath); err != nil {
		t.Fatalf("CopySelf over a running binary: %v", err)
	}

	selfPath, err := os.Executable()
	if err != nil {
		t.Fatalf("locating self: %v", err)
	}
	if !sameContents(selfPath, victimPath) {
		t.Fatalf("target was not replaced with a copy of self")
	}
	if cmd.ProcessState != nil {
		t.Fatalf("running victim died during the swap")
	}
}

func TestCopySelfIdenticalIsNoop(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "copy")

	if _, err := CopySelf(target); err != nil {
		t.Fatalf("first copy: %v", err)
	}
	firstStat, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat after first copy: %v", err)
	}

	if _, err := CopySelf(target); err != nil {
		t.Fatalf("second copy: %v", err)
	}
	secondStat, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat after second copy: %v", err)
	}
	if !firstStat.ModTime().Equal(secondStat.ModTime()) {
		t.Fatalf("identical target was rewritten")
	}
}
