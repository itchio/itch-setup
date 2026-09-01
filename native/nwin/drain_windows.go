package nwin

import (
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/itchio/itch-setup/setup"
)

// ListProcessModulePaths enumerates all processes along with the paths of
// their executable and loaded modules. Processes we can't inspect (access
// denied, exited mid-enumeration) are skipped or reported with whatever
// paths could be determined.
func ListProcessModulePaths() ([]setup.RunningProcess, error) {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snapshot)

	var procs []setup.RunningProcess

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	err = windows.Process32First(snapshot, &entry)
	for err == nil {
		pid := entry.ProcessID
		if pid != 0 {
			paths := listModulePaths(pid)
			if len(paths) == 0 {
				if exePath := queryProcessImagePath(pid); exePath != "" {
					paths = []string{exePath}
				}
			}
			if len(paths) > 0 {
				procs = append(procs, setup.RunningProcess{
					PID:   int(pid),
					Paths: paths,
				})
			}
		}
		err = windows.Process32Next(snapshot, &entry)
	}

	return procs, nil
}

// listModulePaths returns the executable and module paths of a process,
// or nil if the process can't be inspected.
func listModulePaths(pid uint32) []string {
	var snapshot windows.Handle
	var err error

	// CreateToolhelp32Snapshot can transiently fail with ERROR_BAD_LENGTH
	// while the target process is loading modules; MSDN says to retry.
	for tries := 0; tries < 5; tries++ {
		snapshot, err = windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPMODULE|windows.TH32CS_SNAPMODULE32, pid)
		if err != windows.ERROR_BAD_LENGTH {
			break
		}
	}
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snapshot)

	var paths []string
	var entry windows.ModuleEntry32
	entry.Size = uint32(windows.SizeofModuleEntry32)
	err = windows.Module32First(snapshot, &entry)
	for err == nil {
		paths = append(paths, windows.UTF16ToString(entry.ExePath[:]))
		err = windows.Module32Next(snapshot, &entry)
	}

	return paths
}

func queryProcessImagePath(pid uint32) string {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(handle)

	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	err = windows.QueryFullProcessImageName(handle, 0, &buf[0], &size)
	if err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}
