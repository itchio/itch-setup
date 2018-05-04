package nwin

import (
	"errors"
	"syscall"

	"github.com/lxn/win"
)

type Folders struct {
	LocalAppData   string
	RoamingAppData string
	Desktop        string
}

func GetFolders() (f Folders, err error) {
	f.LocalAppData, err = getUserDirectory(win.CSIDL_LOCAL_APPDATA)
	if err != nil {
		return
	}

	f.RoamingAppData, err = getUserDirectory(win.CSIDL_APPDATA)
	if err != nil {
		return
	}

	f.Desktop, err = getUserDirectory(win.CSIDL_DESKTOP)
	if err != nil {
		return
	}

	return
}

func getUserDirectory(csidl win.CSIDL) (string, error) {
	localPathPtr := make([]uint16, 65536+2)
	var hwnd win.HWND
	success := win.SHGetSpecialFolderPath(hwnd, &localPathPtr[0], csidl, true)
	if !success {
		return "", errors.New("Could not get folder path")
	}
	return syscall.UTF16ToString(localPathPtr), nil
}
