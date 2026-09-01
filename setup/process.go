package setup

import (
	"context"
	"log"
	"time"

	ps "github.com/mitchellh/go-ps"
)

// WaitForProcessToExit waits for the process with the given PID to exit.
// It returns nil once the process is gone, or the context's error if the
// context is cancelled first.
//
// The ReadyToRelaunch message is emitted immediately: the app waits for it
// before quitting, so it must not depend on us successfully observing the
// process first.
func WaitForProcessToExit(ctx context.Context, pid int) error {
	retryDuration := 1 * time.Second

	log.Printf("Looking for PID %d", pid)
	EnableJSON()
	defer DisableJSON()

	Emit(ReadyToRelaunch{})

	for {
		select {
		case <-ctx.Done():
			log.Printf("Giving up waiting for PID %d: %v", pid, ctx.Err())
			return ctx.Err()
		default:
			// keep waiting
		}

		proc, err := ps.FindProcess(pid)
		if err != nil {
			log.Printf("While finding process: %+v", err)
			log.Printf("Retrying in %s", retryDuration)
			time.Sleep(retryDuration)
			continue
		}

		if proc == nil {
			log.Printf("Process exited!")
			return nil
		}

		log.Printf("Process still exists (%s)", proc.Executable())
		log.Printf("Retrying in %s", retryDuration)
		time.Sleep(retryDuration)
	}
}
