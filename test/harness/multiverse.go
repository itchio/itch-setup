package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// MultiverseState represents the state.json format
type MultiverseState struct {
	Current string `json:"current"`
	Ready   string `json:"ready"`
}

// MultiverseSetup helps create test directory structures
type MultiverseSetup struct {
	t       *testing.T
	baseDir string
	appName string
}

// NewMultiverseSetup creates a new multiverse setup helper
func NewMultiverseSetup(t *testing.T, tempDir, appName string) *MultiverseSetup {
	t.Helper()

	baseDir := filepath.Join(tempDir, fmt.Sprintf(".%s", appName))
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		t.Fatalf("Failed to create base dir: %v", err)
	}

	return &MultiverseSetup{
		t:       t,
		baseDir: baseDir,
		appName: appName,
	}
}

// BaseDir returns the base directory path (e.g., ~/.itch)
func (m *MultiverseSetup) BaseDir() string {
	return m.baseDir
}

// SetState writes the state.json file
func (m *MultiverseSetup) SetState(current, ready string) {
	m.t.Helper()

	state := MultiverseState{
		Current: current,
		Ready:   ready,
	}

	data, err := json.Marshal(state)
	if err != nil {
		m.t.Fatalf("Failed to marshal state: %v", err)
	}

	statePath := filepath.Join(m.baseDir, "state.json")
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		m.t.Fatalf("Failed to write state.json: %v", err)
	}
}

// CreateAppVersion creates a mock app installation
func (m *MultiverseSetup) CreateAppVersion(version string) string {
	m.t.Helper()

	appDir := filepath.Join(m.baseDir, fmt.Sprintf("app-%s", version))
	if err := os.MkdirAll(appDir, 0755); err != nil {
		m.t.Fatalf("Failed to create app dir: %v", err)
	}

	// Create mock executable
	exePath := filepath.Join(appDir, m.appName)
	script := fmt.Sprintf("#!/bin/sh\necho '%s version %s'\n", m.appName, version)
	if err := os.WriteFile(exePath, []byte(script), 0755); err != nil {
		m.t.Fatalf("Failed to write mock executable: %v", err)
	}

	return appDir
}

// CreateFullSetup creates a complete multiverse with current version installed
func (m *MultiverseSetup) CreateFullSetup(currentVersion string) {
	m.t.Helper()

	m.CreateAppVersion(currentVersion)
	m.SetState(currentVersion, "")
}

// CreateWithReadyPending creates a multiverse with a pending ready version
func (m *MultiverseSetup) CreateWithReadyPending(currentVersion, readyVersion string) {
	m.t.Helper()

	m.CreateAppVersion(currentVersion)
	m.CreateAppVersion(readyVersion)
	m.SetState(currentVersion, readyVersion)
}

// ReadState reads the current state.json
func (m *MultiverseSetup) ReadState() *MultiverseState {
	m.t.Helper()

	statePath := filepath.Join(m.baseDir, "state.json")
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		m.t.Fatalf("Failed to read state.json: %v", err)
	}

	var state MultiverseState
	if err := json.Unmarshal(data, &state); err != nil {
		m.t.Fatalf("Failed to unmarshal state.json: %v", err)
	}

	return &state
}
