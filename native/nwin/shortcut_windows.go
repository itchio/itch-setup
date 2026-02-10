package nwin

// #cgo LDFLAGS: -lole32 -luuid -lpropsys
// #include "shortcut.h"
import "C"

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

type ShortcutSettings struct {
	ShortcutFilePath string
	TargetPath       string
	Arguments        string
	Description      string
	IconLocation     string
	WorkingDirectory string
	OnlyIfExists     bool
	AppUserModelId   string
}

// CreateShortcut creates a windows shortcut with the given settings
func CreateShortcut(settings ShortcutSettings) error {
	if !filepath.IsAbs(settings.ShortcutFilePath) {
		return fmt.Errorf("Shortcut file path is not absolute: %q", settings.ShortcutFilePath)
	}

	if settings.OnlyIfExists {
		_, err := os.Stat(settings.ShortcutFilePath)
		if err != nil {
			log.Printf("Not updating shortcut (%s): %v", settings.ShortcutFilePath, err)
			return nil
		}
	}

	dir := filepath.Dir(settings.ShortcutFilePath)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	shortcutPath := windows.StringToUTF16Ptr(settings.ShortcutFilePath)
	targetPath := windows.StringToUTF16Ptr(settings.TargetPath)
	arguments := windows.StringToUTF16Ptr(settings.Arguments)
	description := windows.StringToUTF16Ptr(settings.Description)
	iconLocation := windows.StringToUTF16Ptr(settings.IconLocation)
	workingDirectory := windows.StringToUTF16Ptr(settings.WorkingDirectory)

	var appUserModelId *uint16
	if settings.AppUserModelId != "" {
		appUserModelId = windows.StringToUTF16Ptr(settings.AppUserModelId)
	}

	hr := C.CreateShortcutWithAppId(
		(*C.wchar_t)(unsafe.Pointer(shortcutPath)),
		(*C.wchar_t)(unsafe.Pointer(targetPath)),
		(*C.wchar_t)(unsafe.Pointer(arguments)),
		(*C.wchar_t)(unsafe.Pointer(description)),
		(*C.wchar_t)(unsafe.Pointer(iconLocation)),
		(*C.wchar_t)(unsafe.Pointer(workingDirectory)),
		(*C.wchar_t)(unsafe.Pointer(appUserModelId)),
	)

	if hr != 0 {
		return fmt.Errorf("CreateShortcutWithAppId failed with HRESULT 0x%08X", uint32(hr))
	}

	return nil
}
