//go:build windows

package rungame

import "syscall"

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

// itch-setup is a windowsgui binary; without this, spawning the console
// butler binary would flash a console window.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
}
