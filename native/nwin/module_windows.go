package nwin

import (
	"errors"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

var getModuleFileNameExProc *syscall.Proc

func GetModuleFileName(hprocess syscall.Handle) (string, error) {
	if getModuleFileNameExProc == nil {
		var fErr error
		var dll *syscall.DLL

		dll, fErr = syscall.LoadDLL("kernel.dll")
		if fErr == nil {
			getModuleFileNameExProc, fErr = dll.FindProc("GetModuleFileNameExW")
		}

		if fErr != nil || getModuleFileNameExProc == nil {
			dll, fErr = syscall.LoadDLL("psapi.dll")
			if fErr == nil {
				getModuleFileNameExProc, fErr = dll.FindProc("GetModuleFileNameExW")
			}
		}
	}

	if getModuleFileNameExProc == nil {
		return "", errors.New("Couldn't find GetModuleFileNameExW")
	}

	var n uint32
	b := make([]uint16, syscall.MAX_PATH)
	size := uint32(len(b))

	r0, _, e1 := getModuleFileNameExProc.Call(uintptr(hprocess), uintptr(0), uintptr(unsafe.Pointer(&b[0])), uintptr(size))
	n = uint32(r0)
	if n == 0 {
		return "", e1
	}
	return string(utf16.Decode(b[0:n])), nil
}
