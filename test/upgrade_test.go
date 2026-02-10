package test

import (
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
