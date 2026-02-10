//go:build nogtk
// +build nogtk

package nlinux

import (
	"log"

	"github.com/itchio/itch-setup/cl"
)

// NewGtkUI falls back to text mode when built without GTK support.
func NewGtkUI(cli cl.CLI) NativeUI {
	log.Printf("GTK support not built in, falling back to text UI")
	return NewTextUI(cli)
}
