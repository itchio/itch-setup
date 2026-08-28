//go:build !windows

package rungame

import "syscall"

// butler must not share our process group: a terminal Ctrl+C would reach
// it directly and our forwarded copy would be its second signal, which
// butler treats as "exit immediately", skipping game shutdown and the
// play session save. In its own group it only ever gets the forward.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// No Unix equivalent of the Windows job object; a SIGKILLed itch-setup
// still orphans butler here.
func tieProcessLifetime(pid int) {}
