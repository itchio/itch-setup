package harness

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

// MockServer simulates broth.itch.zone for testing
type MockServer struct {
	t         *testing.T
	server    *httptest.Server
	latestVer map[string]string        // channel -> version
	builds    map[string]*MockBuild    // "channel/version" -> build info
	archives  map[string][]byte        // "channel/version" -> zip data
	mux       *http.ServeMux
}

// MockBuild represents build info returned by the /info endpoint
type MockBuild struct {
	Version string          `json:"version"`
	Files   []MockBuildFile `json:"files"`
}

// MockBuildFile represents a file in the build
type MockBuildFile struct {
	Type    string `json:"type"`
	SubType string `json:"subType"`
	Size    int64  `json:"size"`
}

// NewMockServer creates a new mock broth server
func NewMockServer(t *testing.T) *MockServer {
	t.Helper()

	ms := &MockServer{
		t:         t,
		latestVer: make(map[string]string),
		builds:    make(map[string]*MockBuild),
		archives:  make(map[string][]byte),
		mux:       http.NewServeMux(),
	}

	ms.mux.HandleFunc("/", ms.handleRequest)
	ms.server = httptest.NewServer(ms.mux)

	return ms
}

// URL returns the mock server's URL
func (ms *MockServer) URL() string {
	return ms.server.URL
}

// Close shuts down the mock server
func (ms *MockServer) Close() {
	ms.server.Close()
}

// channelName returns the channel name for the current platform
func channelName() string {
	os := runtime.GOOS
	arch := runtime.GOARCH
	return fmt.Sprintf("%s-%s", os, arch)
}

// SetLatestVersion sets the latest version for an app's channel
func (ms *MockServer) SetLatestVersion(appName, version string) {
	channel := channelName()
	key := fmt.Sprintf("%s/%s", appName, channel)
	ms.latestVer[key] = version
}

// SetBuildInfo sets the build info for a specific version
func (ms *MockServer) SetBuildInfo(appName, version string, archiveSize int64) {
	channel := channelName()
	key := fmt.Sprintf("%s/%s/%s", appName, channel, version)
	ms.builds[key] = &MockBuild{
		Version: version,
		Files: []MockBuildFile{
			{
				Type:    "archive",
				SubType: "default",
				Size:    archiveSize,
			},
		},
	}
}

// SetArchive sets the archive data for a specific version
func (ms *MockServer) SetArchive(appName, version string, data []byte) {
	channel := channelName()
	key := fmt.Sprintf("%s/%s/%s", appName, channel, version)
	ms.archives[key] = data
}

// CreateMockArchive creates a minimal zip archive with a mock executable
func (ms *MockServer) CreateMockArchive(appName string) []byte {
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)

	// Create mock executable
	f, err := w.Create(appName)
	if err != nil {
		ms.t.Fatalf("Failed to create zip entry: %v", err)
	}

	// Write a simple shell script as the executable
	script := fmt.Sprintf("#!/bin/sh\necho '%s mock executable'\n", appName)
	if _, err := f.Write([]byte(script)); err != nil {
		ms.t.Fatalf("Failed to write zip content: %v", err)
	}

	if err := w.Close(); err != nil {
		ms.t.Fatalf("Failed to close zip: %v", err)
	}

	return buf.Bytes()
}

func (ms *MockServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")
	parts := strings.Split(path, "/")

	ms.t.Logf("Mock server request: %s %s", r.Method, r.URL.Path)

	if len(parts) < 2 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	appName := parts[0]
	channel := parts[1]
	channelKey := fmt.Sprintf("%s/%s", appName, channel)

	// Handle /{app}/{channel}/LATEST
	if len(parts) == 3 && parts[2] == "LATEST" {
		if r.Method == "HEAD" {
			if _, ok := ms.latestVer[channelKey]; ok {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
			return
		}

		version, ok := ms.latestVer[channelKey]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Write([]byte(version))
		return
	}

	// Handle /{app}/{channel}/{version}/...
	if len(parts) >= 4 {
		version := parts[2]
		buildKey := fmt.Sprintf("%s/%s/%s", appName, channel, version)

		// /{app}/{channel}/{version}/info
		if parts[3] == "info" {
			build, ok := ms.builds[buildKey]
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(build)
			return
		}

		// /{app}/{channel}/{version}/archive/default
		if len(parts) == 5 && parts[3] == "archive" && parts[4] == "default" {
			data, ok := ms.archives[buildKey]
			if !ok {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/zip")
			w.Write(data)
			return
		}

		// /{app}/{channel}/{version}/signature/default - return empty signature for now
		if len(parts) == 5 && parts[3] == "signature" && parts[4] == "default" {
			// Return a minimal valid signature placeholder
			// In real tests, we might need to generate actual signatures
			http.Error(w, "signature not implemented", http.StatusNotImplemented)
			return
		}
	}

	ms.t.Logf("Unhandled request: %s", path)
	http.Error(w, "not found", http.StatusNotFound)
}
