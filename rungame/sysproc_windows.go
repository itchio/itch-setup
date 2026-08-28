//go:build windows

package rungame

import (
	"log"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

// itch-setup is a windowsgui binary; without this, spawning the console
// butler binary would flash a console window.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}

// tieProcessLifetime puts butler in a kill-on-close job object, so
// force-terminating itch-setup (Task Manager, a launcher's stop button)
// kills butler instead of orphaning it; butler's own job object then
// does the same for the game. The job handle is deliberately never
// closed: closing it is what kills. Best effort; on failure butler
// simply runs untied.
func tieProcessLifetime(pid int) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		log.Printf("Could not create job object: %v", err)
		return
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err == nil {
		var proc windows.Handle
		proc, err = windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
		if err == nil {
			err = windows.AssignProcessToJobObject(job, proc)
			windows.CloseHandle(proc)
		}
	}
	if err != nil {
		log.Printf("Could not tie butler's lifetime to ours: %v", err)
		windows.CloseHandle(job)
	}
}
