package setup

import (
	"context"
	"log"
	"time"

	ps "github.com/mitchellh/go-ps"
)

func WaitForProcessToExit(ctx context.Context, pid int) {
	retryDuration := 2 * time.Second

	log.Printf("Looking for PID %d", pid)

	for {
		select {
		case <-ctx.Done():
			log.Printf("Wait cancelled!")
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
			return
		} else {
			log.Printf("Process still exists (%s)", proc.Executable())
			log.Printf("Retrying in %s", retryDuration)
			time.Sleep(retryDuration)
			continue
		}
	}
}
