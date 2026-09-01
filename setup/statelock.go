package setup

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// stateLock serializes multiverse state transitions across processes: an
// upgrade queueing a ready build can race a relaunch promoting the
// previous one, and refreshing state.json alone leaves a window where the
// loser overwrites the winner's state or deletes its folder.
type stateLock struct {
	f *os.File
}

// var so tests can shorten it
var stateLockTimeout = 30 * time.Second

// acquireStateLock takes an exclusive cross-process lock for the given
// install root. The lock is released automatically if the process dies.
// Returns (nil, nil) when this platform or filesystem can't lock, in
// which case callers proceed unsynchronized, as they always did.
// Contention past the timeout is an error: whoever holds the lock is
// mid-transition, and barging in is exactly what the lock prevents.
func acquireStateLock(baseDir string) (*stateLock, error) {
	path := filepath.Join(baseDir, "state.lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		log.Printf("Could not open state lock (%s): %v", path, err)
		return nil, nil
	}

	deadline := time.Now().Add(stateLockTimeout)
	for {
		acquired, err := tryLockFile(f)
		if err != nil {
			log.Printf("Could not lock (%s): %v", path, err)
			f.Close()
			return nil, nil
		}
		if acquired {
			return &stateLock{f: f}, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("timed out waiting for state lock (%s)", path)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (l *stateLock) release() {
	unlockFile(l.f)
	l.f.Close()
}
