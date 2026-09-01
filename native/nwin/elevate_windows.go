package nwin

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	ps "github.com/mitchellh/go-ps"
	"golang.org/x/sys/windows"
)

var (
	modadvapi32                 = windows.NewLazySystemDLL("advapi32.dll")
	procCreateProcessWithTokenW = modadvapi32.NewProc("CreateProcessWithTokenW")
	procImpersonateLoggedOnUser = modadvapi32.NewProc("ImpersonateLoggedOnUser")
)

const logonWithProfile = 0x00000001

// IsElevated returns true if this process runs with a full administrator
// token (UAC-elevated, or UAC disabled for an admin account).
func IsElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// RelaunchElevated starts this executable again through UAC with the
// given arguments and returns without waiting for it. The user
// declining the prompt is reported as ErrElevationCancelled.
func RelaunchElevated(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("finding own executable: %w", err)
	}

	verb, _ := windows.UTF16PtrFromString("runas")
	exe16, _ := windows.UTF16PtrFromString(exe)
	args16, _ := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	cwd16, _ := windows.UTF16PtrFromString(filepath.Dir(exe))

	log.Printf("Re-executing elevated: %s %s", exe, strings.Join(args, " "))
	err = windows.ShellExecute(0, verb, exe16, args16, cwd16, windows.SW_SHOWNORMAL)
	if err != nil {
		if errno, ok := err.(syscall.Errno); ok && errno == windows.ERROR_CANCELLED {
			return ErrElevationCancelled
		}
		return fmt.Errorf("ShellExecute runas: %w", err)
	}
	return nil
}

var ErrElevationCancelled = fmt.Errorf("elevation cancelled by user")

// desktopUserToken returns a primary token for the interactive user of
// this session, taken from their explorer.exe. From an elevated process
// this is the way to get the ordinary (medium integrity) user token: the
// linked token of an elevated token is only granted at identification
// level, which can't start processes.
func desktopUserToken() (windows.Token, error) {
	var ourSession uint32
	err := windows.ProcessIdToSessionId(uint32(os.Getpid()), &ourSession)
	if err != nil {
		return 0, fmt.Errorf("ProcessIdToSessionId: %w", err)
	}

	procs, err := ps.Processes()
	if err != nil {
		return 0, fmt.Errorf("listing processes: %w", err)
	}

	for _, p := range procs {
		if !strings.EqualFold(p.Executable(), "explorer.exe") {
			continue
		}
		pid := uint32(p.Pid())

		var session uint32
		if err := windows.ProcessIdToSessionId(pid, &session); err != nil || session != ourSession {
			continue
		}

		h, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION, false, pid)
		if err != nil {
			log.Printf("Could not open explorer.exe (PID %d): %v", pid, err)
			continue
		}

		var tok windows.Token
		err = windows.OpenProcessToken(h, windows.TOKEN_DUPLICATE|windows.TOKEN_QUERY, &tok)
		windows.CloseHandle(h)
		if err != nil {
			log.Printf("Could not open token of explorer.exe (PID %d): %v", pid, err)
			continue
		}

		var primary windows.Token
		err = windows.DuplicateTokenEx(tok,
			windows.TOKEN_QUERY|windows.TOKEN_ASSIGN_PRIMARY|windows.TOKEN_DUPLICATE|windows.TOKEN_ADJUST_DEFAULT|windows.TOKEN_ADJUST_SESSIONID|windows.TOKEN_IMPERSONATE,
			nil, windows.SecurityImpersonation, windows.TokenPrimary, &primary)
		tok.Close()
		if err != nil {
			return 0, fmt.Errorf("DuplicateTokenEx: %w", err)
		}
		return primary, nil
	}

	return 0, fmt.Errorf("no explorer.exe found in session %d", ourSession)
}

// enablePrivilege turns on a privilege in our own token. Ignoring
// failure is fine for callers: the subsequent API call reports the real
// problem.
func enablePrivilege(name string) error {
	var tok windows.Token
	err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_ADJUST_PRIVILEGES|windows.TOKEN_QUERY, &tok)
	if err != nil {
		return err
	}
	defer tok.Close()

	name16, _ := windows.UTF16PtrFromString(name)
	var luid windows.LUID
	err = windows.LookupPrivilegeValue(nil, name16, &luid)
	if err != nil {
		return err
	}

	tp := windows.Tokenprivileges{
		PrivilegeCount: 1,
		Privileges: [1]windows.LUIDAndAttributes{
			{Luid: luid, Attributes: windows.SE_PRIVILEGE_ENABLED},
		},
	}
	return windows.AdjustTokenPrivileges(tok, false, &tp, 0, nil, nil)
}

// StartProcessAsDesktopUser launches exe as the ordinary interactive
// user, for when we run elevated and must not hand our administrator
// token down to the app.
func StartProcessAsDesktopUser(exe string, args []string, dir string) error {
	if err := enablePrivilege("SeIncreaseQuotaPrivilege"); err != nil {
		log.Printf("Could not enable SeIncreaseQuotaPrivilege: %v", err)
	}

	tok, err := desktopUserToken()
	if err != nil {
		return err
	}
	defer tok.Close()

	var env *uint16
	err = windows.CreateEnvironmentBlock(&env, tok, false)
	if err != nil {
		return fmt.Errorf("CreateEnvironmentBlock: %w", err)
	}
	defer windows.DestroyEnvironmentBlock(env)

	exe16, _ := windows.UTF16PtrFromString(exe)
	cmdline16, _ := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{exe}, args...)))
	var dir16 *uint16
	if dir != "" {
		dir16, _ = windows.UTF16PtrFromString(dir)
	}

	si := windows.StartupInfo{}
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation

	r1, _, e1 := procCreateProcessWithTokenW.Call(
		uintptr(tok),
		uintptr(logonWithProfile),
		uintptr(unsafe.Pointer(exe16)),
		uintptr(unsafe.Pointer(cmdline16)),
		uintptr(windows.CREATE_UNICODE_ENVIRONMENT),
		uintptr(unsafe.Pointer(env)),
		uintptr(unsafe.Pointer(dir16)),
		uintptr(unsafe.Pointer(&si)),
		uintptr(unsafe.Pointer(&pi)),
	)
	if r1 == 0 {
		return fmt.Errorf("CreateProcessWithTokenW: %w", e1)
	}
	windows.CloseHandle(pi.Thread)
	windows.CloseHandle(pi.Process)
	return nil
}

// DesktopUserCanWrite reports whether the ordinary interactive user can
// create files in dir. It only means something when we run elevated: an
// elevated installer can write anywhere, but later updates run as the
// plain user.
func DesktopUserCanWrite(dir string) (bool, error) {
	tok, err := desktopUserToken()
	if err != nil {
		return false, err
	}
	defer tok.Close()

	// impersonation needs the impersonation-level duplicate, not primary
	var imp windows.Token
	err = windows.DuplicateTokenEx(tok, windows.TOKEN_QUERY|windows.TOKEN_IMPERSONATE,
		nil, windows.SecurityImpersonation, windows.TokenImpersonation, &imp)
	if err != nil {
		return false, fmt.Errorf("DuplicateTokenEx: %w", err)
	}
	defer imp.Close()

	// impersonation is per OS thread, and the file operations below
	// have to happen on the same thread
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	r1, _, e1 := procImpersonateLoggedOnUser.Call(uintptr(imp))
	if r1 == 0 {
		return false, fmt.Errorf("ImpersonateLoggedOnUser: %w", e1)
	}
	defer windows.RevertToSelf()

	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return false, nil
	}
	f.Close()
	os.Remove(f.Name())
	return true, nil
}
