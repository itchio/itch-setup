package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func makeTestMultiverse(t *testing.T, current string, ready string) (Multiverse, string) {
	t.Helper()
	baseDir := t.TempDir()

	for _, version := range []string{current, ready} {
		if version == "" {
			continue
		}
		dir := filepath.Join(baseDir, "app-"+version)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "itch"), []byte(version), 0755); err != nil {
			t.Fatal(err)
		}
	}

	writeTestState(t, baseDir, current, ready)

	mv, err := NewMultiverse(&MultiverseParams{
		AppName: "itch",
		BaseDir: baseDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	return mv, baseDir
}

func writeTestState(t *testing.T, baseDir string, current string, ready string) {
	t.Helper()
	bs, err := json.Marshal(&multiverseState{Current: current, Ready: ready})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, "state.json"), bs, 0644); err != nil {
		t.Fatal(err)
	}
}

func readTestState(t *testing.T, baseDir string) *multiverseState {
	t.Helper()
	bs, err := os.ReadFile(filepath.Join(baseDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	state := &multiverseState{}
	if err := json.Unmarshal(bs, state); err != nil {
		t.Fatal(err)
	}
	return state
}

func makeStagedBuild(t *testing.T, version string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "app-"+version)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "itch"), []byte(version), 0755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func dirExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func Test_QueueReady_SupersedesOldReady(t *testing.T) {
	mv, baseDir := makeTestMultiverse(t, "1.0.0", "2.0.0")

	err := mv.QueueReady(&BuildFolder{
		Version: "3.0.0",
		Path:    makeStagedBuild(t, "3.0.0"),
	})
	if err != nil {
		t.Fatal(err)
	}

	state := readTestState(t, baseDir)
	if state.Current != "1.0.0" || state.Ready != "3.0.0" {
		t.Errorf("expected state {1.0.0, 3.0.0}, got {%s, %s}", state.Current, state.Ready)
	}
	if !dirExists(filepath.Join(baseDir, "app-3.0.0")) {
		t.Error("expected app-3.0.0 to exist")
	}
	if dirExists(filepath.Join(baseDir, "app-2.0.0")) {
		t.Error("expected superseded app-2.0.0 to be removed")
	}
	if !dirExists(filepath.Join(baseDir, "app-1.0.0")) {
		t.Error("expected current app-1.0.0 to be untouched")
	}
}

func Test_QueueReady_RefreshesExternallyChangedState(t *testing.T) {
	mv, baseDir := makeTestMultiverse(t, "1.0.0", "2.0.0")

	// while this multiverse held its state in memory, a relaunch promoted
	// the ready build and rewrote state.json
	writeTestState(t, baseDir, "2.0.0", "")

	err := mv.QueueReady(&BuildFolder{
		Version: "3.0.0",
		Path:    makeStagedBuild(t, "3.0.0"),
	})
	if err != nil {
		t.Fatal(err)
	}

	state := readTestState(t, baseDir)
	if state.Current != "2.0.0" {
		t.Errorf("expected the promoted current 2.0.0 to be preserved, got %s", state.Current)
	}
	if state.Ready != "3.0.0" {
		t.Errorf("expected ready 3.0.0, got %s", state.Ready)
	}
	if !dirExists(filepath.Join(baseDir, "app-2.0.0")) {
		t.Error("expected promoted app-2.0.0 to be untouched")
	}
}

func Test_ClearReady_RemovesBuildAndState(t *testing.T) {
	mv, baseDir := makeTestMultiverse(t, "3.0.0", "2.0.0")

	if err := mv.ClearReady(); err != nil {
		t.Fatal(err)
	}

	state := readTestState(t, baseDir)
	if state.Current != "3.0.0" || state.Ready != "" {
		t.Errorf("expected state {3.0.0, \"\"}, got {%s, %s}", state.Current, state.Ready)
	}
	if dirExists(filepath.Join(baseDir, "app-2.0.0")) {
		t.Error("expected discarded app-2.0.0 to be removed")
	}
	if !dirExists(filepath.Join(baseDir, "app-3.0.0")) {
		t.Error("expected current app-3.0.0 to be untouched")
	}
}

func Test_ClearReady_KeepsFolderSharedWithCurrent(t *testing.T) {
	mv, baseDir := makeTestMultiverse(t, "3.0.0", "3.0.0")

	if err := mv.ClearReady(); err != nil {
		t.Fatal(err)
	}

	state := readTestState(t, baseDir)
	if state.Current != "3.0.0" || state.Ready != "" {
		t.Errorf("expected state {3.0.0, \"\"}, got {%s, %s}", state.Current, state.Ready)
	}
	if !dirExists(filepath.Join(baseDir, "app-3.0.0")) {
		t.Error("expected current app-3.0.0 to survive clearing a ready that shares its folder")
	}
}

func Test_QueueReady_FailsClosedOnUnreadableState(t *testing.T) {
	mv, baseDir := makeTestMultiverse(t, "1.0.0", "2.0.0")

	// a directory where state.json should be makes reads fail; the
	// destructive parts of the transition must not run on cached state
	statePath := filepath.Join(baseDir, "state.json")
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(statePath, 0755); err != nil {
		t.Fatal(err)
	}

	stagedPath := makeStagedBuild(t, "3.0.0")
	err := mv.QueueReady(&BuildFolder{
		Version: "3.0.0",
		Path:    stagedPath,
	})
	if err == nil {
		t.Fatal("expected QueueReady to fail when state can't be read")
	}

	if got := mv.GetReadyVersion(); got != "2.0.0" {
		t.Errorf("expected in-memory ready to stay 2.0.0, got %q", got)
	}
	if !dirExists(filepath.Join(baseDir, "app-2.0.0")) {
		t.Error("expected old ready app-2.0.0 to be untouched")
	}
	if dirExists(filepath.Join(baseDir, "app-3.0.0")) {
		t.Error("expected new build not to be moved into place")
	}
	if !dirExists(stagedPath) {
		t.Error("expected staged build to remain where it was")
	}
}

func Test_QueueReady_HealsCorruptState(t *testing.T) {
	mv, baseDir := makeTestMultiverse(t, "1.0.0", "2.0.0")

	// a corrupt state file must not block transitions: rewriting it is
	// the only way it heals, and reinstalls go through QueueReady
	statePath := filepath.Join(baseDir, "state.json")
	if err := os.WriteFile(statePath, []byte("garbage{"), 0644); err != nil {
		t.Fatal(err)
	}

	err := mv.QueueReady(&BuildFolder{
		Version: "3.0.0",
		Path:    makeStagedBuild(t, "3.0.0"),
	})
	if err != nil {
		t.Fatalf("expected QueueReady to proceed past a corrupt state file, got: %v", err)
	}

	state := readTestState(t, baseDir)
	if state.Current != "1.0.0" || state.Ready != "3.0.0" {
		t.Errorf("expected healed state {1.0.0, 3.0.0}, got {%s, %s}", state.Current, state.Ready)
	}
	if !dirExists(filepath.Join(baseDir, "app-3.0.0")) {
		t.Error("expected app-3.0.0 to be queued")
	}
}

func Test_MakeReadyCurrent_FailsClosedOnUnreadableState(t *testing.T) {
	mv, baseDir := makeTestMultiverse(t, "1.0.0", "2.0.0")

	statePath := filepath.Join(baseDir, "state.json")
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(statePath, 0755); err != nil {
		t.Fatal(err)
	}

	if err := mv.MakeReadyCurrent(); err == nil {
		t.Fatal("expected MakeReadyCurrent to fail when state can't be read")
	}

	if !dirExists(filepath.Join(baseDir, "app-1.0.0")) {
		t.Error("expected current app-1.0.0 to be untouched")
	}
	if !dirExists(filepath.Join(baseDir, "app-2.0.0")) {
		t.Error("expected ready app-2.0.0 to be untouched")
	}
}

func Test_ReloadReadyVersion(t *testing.T) {
	mv, baseDir := makeTestMultiverse(t, "1.0.0", "2.0.0")

	// another process promoted the ready build behind our back
	writeTestState(t, baseDir, "2.0.0", "")

	version, err := mv.ReloadReadyVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != "" {
		t.Errorf("expected no ready version after external promotion, got %q", version)
	}

	writeTestState(t, baseDir, "2.0.0", "3.0.0")
	version, err = mv.ReloadReadyVersion()
	if err != nil {
		t.Fatal(err)
	}
	if version != "3.0.0" {
		t.Errorf("expected ready 3.0.0, got %q", version)
	}

	// unreadable state is an error, not a silent stale answer
	statePath := filepath.Join(baseDir, "state.json")
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(statePath, 0755); err != nil {
		t.Fatal(err)
	}
	_, err = mv.ReloadReadyVersion()
	if err == nil {
		t.Fatal("expected an error for unreadable state")
	}
}

func Test_MakeReadyCurrent_RefreshesExternallyChangedState(t *testing.T) {
	mv, baseDir := makeTestMultiverse(t, "1.0.0", "2.0.0")

	// while this multiverse held its state in memory, an upgrade
	// superseded the ready build
	dir := filepath.Join(baseDir, "app-3.0.0")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(baseDir, "app-2.0.0")); err != nil {
		t.Fatal(err)
	}
	writeTestState(t, baseDir, "1.0.0", "3.0.0")

	if err := mv.MakeReadyCurrent(); err != nil {
		t.Fatal(err)
	}

	state := readTestState(t, baseDir)
	if state.Current != "3.0.0" || state.Ready != "" {
		t.Errorf("expected state {3.0.0, \"\"}, got {%s, %s}", state.Current, state.Ready)
	}
	if !dirExists(filepath.Join(baseDir, "app-3.0.0")) {
		t.Error("expected promoted app-3.0.0 to exist")
	}
	if dirExists(filepath.Join(baseDir, "app-1.0.0")) {
		t.Error("expected old current app-1.0.0 to be cleaned up")
	}
}

func Test_StateLock_ContentionTimesOut(t *testing.T) {
	oldTimeout := stateLockTimeout
	stateLockTimeout = 300 * time.Millisecond
	defer func() { stateLockTimeout = oldTimeout }()

	baseDir := t.TempDir()

	lock, err := acquireStateLock(baseDir)
	if err != nil {
		t.Fatal(err)
	}
	if lock == nil {
		t.Skip("locking not supported here")
	}

	_, err = acquireStateLock(baseDir)
	if err == nil {
		t.Fatal("expected second acquisition to time out while first is held")
	}

	lock.release()

	lock2, err := acquireStateLock(baseDir)
	if err != nil {
		t.Fatalf("expected acquisition to succeed after release, got: %v", err)
	}
	lock2.release()
}

func Test_ClearReady_NoReadyIsANoOp(t *testing.T) {
	mv, baseDir := makeTestMultiverse(t, "1.0.0", "")

	if err := mv.ClearReady(); err != nil {
		t.Fatal(err)
	}

	state := readTestState(t, baseDir)
	if state.Current != "1.0.0" || state.Ready != "" {
		t.Errorf("expected state {1.0.0, \"\"}, got {%s, %s}", state.Current, state.Ready)
	}
}
