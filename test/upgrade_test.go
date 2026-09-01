package test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/itchio/itch-setup/test/harness"
)

func TestUpgrade_NoUpdateAvailable(t *testing.T) {
	h := harness.New(t)
	defer h.Cleanup()

	// Set up current version = server latest version
	mv := harness.NewMultiverseSetup(t, h.TempDir(), "itch")
	mv.CreateFullSetup("1.0.0")

	// Server reports same version as installed
	h.Server().SetLatestVersion("itch", "1.0.0")

	// Run upgrade
	result := h.Run("--appname", "itch", "--upgrade")

	t.Logf("Exit code: %d", result.ExitCode)
	t.Logf("Stdout:\n%s", result.Stdout)
	t.Logf("Stderr:\n%s", result.Stderr)
	t.Logf("Messages: %d", len(result.Messages))
	for i, msg := range result.Messages {
		t.Logf("  [%d] type=%s", i, msg.Type)
	}

	// Should emit no-update-available
	if !result.HasMessageType(harness.TypeNoUpdateAvailable) {
		t.Errorf("Expected no-update-available message, got messages: %v", result.Messages)
	}

	// Should exit cleanly
	if result.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", result.ExitCode)
	}
}

func TestUpgrade_ReadyPending(t *testing.T) {
	h := harness.New(t)
	defer h.Cleanup()

	// Set up current 1.0.0 with ready 2.0.0 already downloaded
	mv := harness.NewMultiverseSetup(t, h.TempDir(), "itch")
	mv.CreateWithReadyPending("1.0.0", "2.0.0")

	// Server reports 2.0.0 as latest
	h.Server().SetLatestVersion("itch", "2.0.0")

	// Run upgrade
	result := h.Run("--appname", "itch", "--upgrade")

	t.Logf("Exit code: %d", result.ExitCode)
	t.Logf("Stdout:\n%s", result.Stdout)
	t.Logf("Stderr:\n%s", result.Stderr)
	t.Logf("Messages: %d", len(result.Messages))
	for i, msg := range result.Messages {
		t.Logf("  [%d] type=%s", i, msg.Type)
	}

	// Should emit update-ready immediately (no download needed)
	if !result.HasMessageType(harness.TypeUpdateReady) {
		t.Errorf("Expected update-ready message, got messages: %v", result.Messages)
	}

	// Should NOT emit installing-update (no download)
	if result.HasMessageType(harness.TypeInstallingUpdate) {
		t.Errorf("Did not expect installing-update message for ready pending")
	}

	// Verify the update-ready contains the correct version
	msg := result.GetFirstMessageOfType(harness.TypeUpdateReady)
	if msg != nil {
		payload, ok := msg.GetUpdateReadyPayload()
		if ok && payload.Version != "2.0.0" {
			t.Errorf("Expected update-ready for version 2.0.0, got %s", payload.Version)
		}
	}

	// Should exit cleanly
	if result.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", result.ExitCode)
	}
}

func TestUpgrade_SupersedeReady(t *testing.T) {
	h := harness.New(t)
	defer h.Cleanup()

	// Current 1.0.0, ready 2.0.0 staged, but the channel moved on to 3.0.0
	mv := harness.NewMultiverseSetup(t, h.TempDir(), "itch")
	mv.CreateWithReadyPending("1.0.0", "2.0.0")

	h.Server().SetLatestVersion("itch", "3.0.0")
	archive := h.Server().CreateMockArchive("itch")
	h.Server().SetBuildInfo("itch", "3.0.0", int64(len(archive)))
	h.Server().SetArchive("itch", "3.0.0", archive)

	result := h.Run("--appname", "itch", "--upgrade")

	t.Logf("Exit code: %d", result.ExitCode)
	t.Logf("Stderr:\n%s", result.Stderr)
	for i, msg := range result.Messages {
		t.Logf("  [%d] type=%s", i, msg.Type)
	}

	// The stale early-return must not be taken: 3.0.0 gets downloaded
	if !result.HasMessageType(harness.TypeInstallingUpdate) {
		t.Errorf("Expected installing-update message, got: %v", result.Messages)
	}

	// update-ready must report the actually staged version
	msg := result.GetFirstMessageOfType(harness.TypeUpdateReady)
	if msg == nil {
		t.Fatalf("Expected update-ready message, got: %v", result.Messages)
	}
	payload, ok := msg.GetUpdateReadyPayload()
	if !ok || payload.Version != "3.0.0" {
		t.Errorf("Expected update-ready for version 3.0.0, got %+v", payload)
	}

	state := mv.ReadState()
	if state.Current != "1.0.0" || state.Ready != "3.0.0" {
		t.Errorf("Expected state {1.0.0, 3.0.0}, got {%s, %s}", state.Current, state.Ready)
	}
	assertDirExists(t, mv.BaseDir(), "app-1.0.0", true)
	assertDirExists(t, mv.BaseDir(), "app-3.0.0", true)
	assertDirExists(t, mv.BaseDir(), "app-2.0.0", false)

	if result.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", result.ExitCode)
	}
}

func TestUpgrade_SupersedeDownloadFails(t *testing.T) {
	h := harness.New(t)
	defer h.Cleanup()

	mv := harness.NewMultiverseSetup(t, h.TempDir(), "itch")
	mv.CreateWithReadyPending("1.0.0", "2.0.0")

	// 3.0.0 is announced with build info, but the archive download 404s
	h.Server().SetLatestVersion("itch", "3.0.0")
	h.Server().SetBuildInfo("itch", "3.0.0", 1024)

	result := h.Run("--appname", "itch", "--upgrade")

	t.Logf("Exit code: %d", result.ExitCode)
	t.Logf("Stderr:\n%s", result.Stderr)
	for i, msg := range result.Messages {
		t.Logf("  [%d] type=%s", i, msg.Type)
	}

	if !result.HasMessageType(harness.TypeUpdateFailed) {
		t.Errorf("Expected update-failed message, got: %v", result.Messages)
	}

	// the previously staged 2.0.0 is still valid and must be reported so
	// the caller ends up in a truthful ready state
	msg := result.GetFirstMessageOfType(harness.TypeUpdateReady)
	if msg == nil {
		t.Fatalf("Expected update-ready for the surviving staged version, got: %v", result.Messages)
	}
	payload, ok := msg.GetUpdateReadyPayload()
	if !ok || payload.Version != "2.0.0" {
		t.Errorf("Expected update-ready for version 2.0.0, got %+v", payload)
	}

	state := mv.ReadState()
	if state.Current != "1.0.0" || state.Ready != "2.0.0" {
		t.Errorf("Expected state {1.0.0, 2.0.0}, got {%s, %s}", state.Current, state.Ready)
	}
	assertDirExists(t, mv.BaseDir(), "app-2.0.0", true)

	if result.ExitCode == 0 {
		t.Errorf("Expected non-zero exit code for failed upgrade")
	}
}

func TestUpgrade_StaleReadyWhenUpToDate(t *testing.T) {
	h := harness.New(t)
	defer h.Cleanup()

	// current already is the latest, but an old ready build lingers; left
	// alone it would be promoted on the next launch, a downgrade
	mv := harness.NewMultiverseSetup(t, h.TempDir(), "itch")
	mv.CreateAppVersion("3.0.0")
	mv.CreateAppVersion("2.0.0")
	mv.SetState("3.0.0", "2.0.0")

	h.Server().SetLatestVersion("itch", "3.0.0")

	result := h.Run("--appname", "itch", "--upgrade")

	t.Logf("Exit code: %d", result.ExitCode)
	t.Logf("Stderr:\n%s", result.Stderr)
	for i, msg := range result.Messages {
		t.Logf("  [%d] type=%s", i, msg.Type)
	}

	if !result.HasMessageType(harness.TypeNoUpdateAvailable) {
		t.Errorf("Expected no-update-available message, got: %v", result.Messages)
	}
	if result.HasMessageType(harness.TypeUpdateReady) {
		t.Errorf("Did not expect update-ready for a stale ready build")
	}

	state := mv.ReadState()
	if state.Current != "3.0.0" || state.Ready != "" {
		t.Errorf("Expected state {3.0.0, \"\"}, got {%s, %s}", state.Current, state.Ready)
	}
	assertDirExists(t, mv.BaseDir(), "app-3.0.0", true)
	assertDirExists(t, mv.BaseDir(), "app-2.0.0", false)

	if result.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", result.ExitCode)
	}
}

func assertDirExists(t *testing.T, baseDir string, name string, want bool) {
	t.Helper()
	_, err := os.Stat(filepath.Join(baseDir, name))
	got := err == nil
	if got != want {
		t.Errorf("Expected %s existence to be %v, got %v", name, want, got)
	}
}

// Note: TestUpgrade_UpdateAvailable requires full archive download which is complex
// to set up with signatures. Keeping it commented for now as the key flows
// (no-update and ready-pending) are tested above.
//
// func TestUpgrade_UpdateAvailable(t *testing.T) {
// 	h := harness.New(t)
// 	defer h.Cleanup()
//
// 	mv := harness.NewMultiverseSetup(t, h.TempDir(), "itch")
// 	mv.CreateFullSetup("1.0.0")
//
// 	// Server reports newer version
// 	h.Server().SetLatestVersion("itch", "2.0.0")
//
// 	// Set up mock archive
// 	archive := h.Server().CreateMockArchive("itch")
// 	h.Server().SetBuildInfo("itch", "2.0.0", int64(len(archive)))
// 	h.Server().SetArchive("itch", "2.0.0", archive)
//
// 	result := h.Run("--appname", "itch", "--upgrade")
//
// 	// Should emit installing-update and update-ready
// 	if !result.HasMessageType(harness.TypeInstallingUpdate) {
// 		t.Errorf("Expected installing-update message")
// 	}
// 	if !result.HasMessageType(harness.TypeUpdateReady) {
// 		t.Errorf("Expected update-ready message")
// 	}
// }
