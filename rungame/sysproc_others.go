//go:build !windows

package rungame

import "syscall"

func sysProcAttr() *syscall.SysProcAttr {
	return nil
}
