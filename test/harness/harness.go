package harness

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Harness manages the test environment for itch-setup
type Harness struct {
	t          *testing.T
	binaryPath string
	tempDir    string
	server     *MockServer
	mu         sync.Mutex
}

// Result holds the output from running itch-setup
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Messages []Message
}

// New creates a new test harness
func New(t *testing.T) *Harness {
	t.Helper()

	// Create temp directory for this test
	tempDir, err := os.MkdirTemp("", "itch-setup-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	h := &Harness{
		t:       t,
		tempDir: tempDir,
	}

	// Build the binary
	h.buildBinary()

	// Start mock server
	h.server = NewMockServer(t)

	return h
}

// buildBinary builds itch-setup for testing
func (h *Harness) buildBinary() {
	h.t.Helper()

	// Get the project root (two levels up from test/harness)
	projectRoot := filepath.Join(h.tempDir, "..", "..", "..")

	// Use a simpler approach - find the project root from the current test
	// The test is run from the project root
	cwd, err := os.Getwd()
	if err != nil {
		h.t.Fatalf("Failed to get working directory: %v", err)
	}

	// Walk up to find go.mod
	projectRoot = cwd
	for {
		if _, err := os.Stat(filepath.Join(projectRoot, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(projectRoot)
		if parent == projectRoot {
			h.t.Fatalf("Could not find project root (go.mod)")
		}
		projectRoot = parent
	}

	h.binaryPath = filepath.Join(h.tempDir, "itch-setup")
	goCache := filepath.Join(os.TempDir(), "itch-setup-go-cache")

	// Build the binary without GTK to keep integration test builds fast.
	cmd := exec.Command("go", "build", "-tags", "nogtk", "-o", h.binaryPath, ".")
	cmd.Dir = projectRoot
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		fmt.Sprintf("GOCACHE=%s", goCache),
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		h.t.Fatalf("Failed to build binary: %v\nOutput: %s", err, output)
	}
}

// TempDir returns the temporary directory for this test
func (h *Harness) TempDir() string {
	return h.tempDir
}

// ServerURL returns the mock server URL
func (h *Harness) ServerURL() string {
	return h.server.URL()
}

// Server returns the mock server for configuration
func (h *Harness) Server() *MockServer {
	return h.server
}

// Run executes itch-setup with the given arguments.
// Always injects --silent to avoid GTK initialization in tests.
func (h *Harness) Run(args ...string) *Result {
	h.t.Helper()
	return h.RunWithEnv(nil, args...)
}

// RunWithEnv executes itch-setup with extra environment variables.
// Always injects --silent to avoid GTK initialization in tests.
func (h *Harness) RunWithEnv(extraEnv map[string]string, args ...string) *Result {
	h.t.Helper()

	// Always run in silent mode to avoid GTK dependency
	fullArgs := append([]string{"--silent"}, args...)
	cmd := exec.Command(h.binaryPath, fullArgs...)

	// Set up environment
	env := []string{
		fmt.Sprintf("HOME=%s", h.tempDir),
		fmt.Sprintf("ITCH_BROTH_URL=%s", h.server.URL()),
		"DISPLAY=:0", // Required for GTK even in silent mode
	}

	// Copy minimal required environment variables
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "PATH=") ||
			strings.HasPrefix(e, "LD_LIBRARY_PATH=") ||
			strings.HasPrefix(e, "PKG_CONFIG_PATH=") {
			env = append(env, e)
		}
	}

	for k, v := range extraEnv {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	cmd.Env = env

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			h.t.Logf("Run error: %v", err)
			result.ExitCode = -1
		}
	}

	// Parse JSON messages from stdout
	result.Messages = ParseMessages(stdout.String())

	return result
}

// ParseMessages extracts JSON messages from stdout
func ParseMessages(stdout string) []Message {
	var messages []Message
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		line := scanner.Text()
		if msg, ok := ParseMessage(line); ok {
			messages = append(messages, msg)
		}
	}
	return messages
}

// Cleanup removes temporary files and stops the server
func (h *Harness) Cleanup() {
	h.server.Close()
	os.RemoveAll(h.tempDir)
}
