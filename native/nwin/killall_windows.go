package nwin

import (
	"log"
	"os"
	"syscall"

	ps "github.com/mitchellh/go-ps"
)

func KillAll(pathsToKill []string) error {
	killMap := make(map[string]struct{})
	for _, ptk := range pathsToKill {
		killMap[ptk] = struct{}{}
	}

	pidsToKill := []int{}

	processes, err := ps.Processes()
	if err != nil {
		return err
	}

	handleProc := func(process ps.Process) {
		handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(process.Pid()))
		if err != nil {
			if errno, ok := err.(syscall.Errno); ok && errno == syscall.ERROR_ACCESS_DENIED {
				// ignore
				return
			}
			log.Printf("Couldn't open process (pid %d): %s", process.Pid(), err.Error())
		} else {
			defer syscall.Close(handle)
			fullName, err := GetModuleFileName(handle)
			if err != nil {
				log.Printf("Couldn't get module file name (pid %d): %s", process.Pid(), err.Error())
			} else {
				if _, ok := killMap[fullName]; ok {
					log.Printf("Should kill %d", process.Pid())
					pidsToKill = append(pidsToKill, process.Pid())
				}
			}
		}
	}

	for _, process := range processes {
		handleProc(process)
	}

	log.Printf("%d processes to kill", len(pidsToKill))
	for _, pidToKill := range pidsToKill {
		func() {
			p, err := os.FindProcess(pidToKill)
			if err != nil {
				// oh well
				log.Printf("PID %d vanished", pidToKill)
				return
			}

			log.Printf("Killing %d...", pidToKill)

			// not even going to bother with the error code - if it works, great! if it doesn't, oh well
			p.Kill()
		}()
	}

	return nil
}
