package setup

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
)

// RunningProcess describes a live process and the executable/module paths
// it holds open, as far as we could determine.
type RunningProcess struct {
	PID int
	// Paths holds the absolute path of the process executable and of any
	// loaded modules (DLLs). Any of these being inside a version folder
	// keeps Windows from renaming that folder.
	Paths []string
}

// ProcessLister enumerates running processes. Implementations should
// tolerate processes vanishing mid-enumeration and skip processes they
// can't inspect (access denied) rather than failing the whole listing.
type ProcessLister func() ([]RunningProcess, error)

var dirQuiescencePollInterval = 1 * time.Second

// FindBlockingProcesses returns the processes (other than excludePID) that
// hold their executable or a loaded module inside dir.
func FindBlockingProcesses(lister ProcessLister, dir string, excludePID int) ([]RunningProcess, error) {
	procs, err := lister()
	if err != nil {
		return nil, err
	}

	var blockers []RunningProcess
	for _, proc := range procs {
		if proc.PID == excludePID {
			continue
		}
		for _, p := range proc.Paths {
			if PathWithinDir(p, dir) {
				blockers = append(blockers, RunningProcess{
					PID:   proc.PID,
					Paths: matchingPaths(proc.Paths, dir),
				})
				break
			}
		}
	}
	return blockers, nil
}

func matchingPaths(paths []string, dir string) []string {
	var out []string
	for _, p := range paths {
		if PathWithinDir(p, dir) {
			out = append(out, p)
		}
	}
	return out
}

// WaitForDirQuiescence polls until no running process (other than
// excludePID) holds executables or modules inside dir, or until ctx is
// done. On timeout it returns an error naming the remaining blockers.
// Lister errors are logged and retried, bounded by ctx.
func WaitForDirQuiescence(ctx context.Context, lister ProcessLister, dir string, excludePID int) error {
	log.Printf("Waiting for processes to release (%s)", dir)

	var lastBlockers []RunningProcess
	for {
		blockers, err := FindBlockingProcesses(lister, dir, excludePID)
		if err != nil {
			log.Printf("Could not enumerate processes: %+v", err)
		} else {
			if len(blockers) == 0 {
				log.Printf("No processes are using (%s) anymore", dir)
				return nil
			}
			lastBlockers = blockers
			for _, b := range blockers {
				log.Printf("Still in use by PID %d (%s)", b.PID, strings.Join(b.Paths, ", "))
			}
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out waiting for processes to release (%s): still in use by %s", dir, describeBlockers(lastBlockers))
		case <-time.After(dirQuiescencePollInterval):
			// keep polling
		}
	}
}

func describeBlockers(blockers []RunningProcess) string {
	if len(blockers) == 0 {
		return "(unknown processes)"
	}
	var descs []string
	for _, b := range blockers {
		descs = append(descs, fmt.Sprintf("PID %d (%s)", b.PID, strings.Join(b.Paths, ", ")))
	}
	return strings.Join(descs, "; ")
}
