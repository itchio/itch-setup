package setup

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

const testAppDir = `C:\kitch\app-26.10.0`

// fakeLister returns canned process lists, advancing through them on each
// call and repeating the last one.
type fakeLister struct {
	mu    sync.Mutex
	calls int
	lists [][]RunningProcess
	errs  []error
}

func (fl *fakeLister) list() ([]RunningProcess, error) {
	fl.mu.Lock()
	defer fl.mu.Unlock()
	i := fl.calls
	fl.calls++
	if i < len(fl.errs) && fl.errs[i] != nil {
		return nil, fl.errs[i]
	}
	if i >= len(fl.lists) {
		i = len(fl.lists) - 1
	}
	return fl.lists[i], nil
}

func Test_FindBlockingProcesses(t *testing.T) {
	lister := (&fakeLister{lists: [][]RunningProcess{{
		// the drainer itself, must be excluded
		{PID: 100, Paths: []string{`C:\kitch\app-26.10.0\kitch.exe`}},
		// a renderer child in the version dir
		{PID: 200, Paths: []string{`C:\kitch\app-26.10.0\kitch.exe`}},
		// a game with the overlay DLL mapped from the version dir
		{PID: 300, Paths: []string{`C:\Games\cool.exe`, `C:\kitch\app-26.10.0\overlay.dll`}},
		// unrelated process with the same exe name elsewhere
		{PID: 400, Paths: []string{`C:\Elsewhere\kitch.exe`}},
		// version-dir sibling that must not match by prefix
		{PID: 500, Paths: []string{`C:\kitch\app-26.1\kitch.exe`}},
	}}}).list

	blockers, err := FindBlockingProcesses(lister, testAppDir, 100)
	if err != nil {
		t.Fatal(err)
	}

	if len(blockers) != 2 {
		t.Fatalf("expected 2 blockers, got %d: %+v", len(blockers), blockers)
	}
	if blockers[0].PID != 200 || blockers[1].PID != 300 {
		t.Errorf("expected PIDs 200 and 300, got %+v", blockers)
	}
	if len(blockers[1].Paths) != 1 || blockers[1].Paths[0] != `C:\kitch\app-26.10.0\overlay.dll` {
		t.Errorf("expected only the blocking path to be reported, got %+v", blockers[1].Paths)
	}
}

func Test_WaitForDirQuiescence_DrainsThenReturns(t *testing.T) {
	oldInterval := dirQuiescencePollInterval
	dirQuiescencePollInterval = 10 * time.Millisecond
	defer func() { dirQuiescencePollInterval = oldInterval }()

	fl := &fakeLister{
		lists: [][]RunningProcess{
			{{PID: 200, Paths: []string{`C:\kitch\app-26.10.0\kitch.exe`}}},
			{{PID: 200, Paths: []string{`C:\kitch\app-26.10.0\kitch.exe`}}},
			{}, // everything exited
		},
		// a transient enumeration error must be tolerated
		errs: []error{nil, errors.New("transient")},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := WaitForDirQuiescence(ctx, fl.list, testAppDir, 100)
	if err != nil {
		t.Fatalf("expected quiescence, got: %+v", err)
	}
	if fl.calls < 3 {
		t.Errorf("expected at least 3 polls, got %d", fl.calls)
	}
}

func Test_WaitForDirQuiescence_TimesOutWithDiagnostics(t *testing.T) {
	oldInterval := dirQuiescencePollInterval
	dirQuiescencePollInterval = 10 * time.Millisecond
	defer func() { dirQuiescencePollInterval = oldInterval }()

	fl := &fakeLister{lists: [][]RunningProcess{
		{{PID: 300, Paths: []string{`C:\Games\cool.exe`, `C:\kitch\app-26.10.0\overlay.dll`}}},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := WaitForDirQuiescence(ctx, fl.list, testAppDir, 100)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	// diagnostics must identify the blocker by PID and path
	if want := "PID 300"; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err.Error(), want)
	}
	if want := `C:\kitch\app-26.10.0\overlay.dll`; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not mention %q", err.Error(), want)
	}
}
