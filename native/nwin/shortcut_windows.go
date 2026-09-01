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
	"strings"
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

	// Empty optional fields are passed as NULL so the C side skips them
	// rather than setting an empty value on the shell link.
	utf16OrNil := func(s string) *uint16 {
		if s == "" {
			return nil
		}
		return windows.StringToUTF16Ptr(s)
	}

	shortcutPath := windows.StringToUTF16Ptr(settings.ShortcutFilePath)
	targetPath := utf16OrNil(settings.TargetPath)
	arguments := utf16OrNil(settings.Arguments)
	description := utf16OrNil(settings.Description)
	iconLocation := utf16OrNil(settings.IconLocation)
	workingDirectory := utf16OrNil(settings.WorkingDirectory)
	appUserModelId := utf16OrNil(settings.AppUserModelId)

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

// GetShortcutTarget reads the target path of an existing .lnk file.
// Returns an empty string (and no error) if the link stores no target.
func GetShortcutTarget(shortcutFilePath string) (string, error) {
	if !filepath.IsAbs(shortcutFilePath) {
		return "", fmt.Errorf("Shortcut file path is not absolute: %q", shortcutFilePath)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	shortcutPath := windows.StringToUTF16Ptr(shortcutFilePath)
	buf := make([]uint16, windows.MAX_LONG_PATH)

	hr := C.GetShortcutTarget(
		(*C.wchar_t)(unsafe.Pointer(shortcutPath)),
		(*C.wchar_t)(unsafe.Pointer(&buf[0])),
		C.int(len(buf)),
	)
	if hr < 0 {
		return "", fmt.Errorf("GetShortcutTarget(%q) failed with HRESULT 0x%08X", shortcutFilePath, uint32(hr))
	}

	target := windows.UTF16ToString(buf)

	// some links store targets with unexpanded environment variables
	if strings.Contains(target, "%") {
		expanded := make([]uint16, windows.MAX_LONG_PATH)
		n, err := windows.ExpandEnvironmentStrings(
			windows.StringToUTF16Ptr(target),
			&expanded[0],
			uint32(len(expanded)),
		)
		if err == nil && n > 0 {
			target = windows.UTF16ToString(expanded)
		}
	}

	return target, nil
}
