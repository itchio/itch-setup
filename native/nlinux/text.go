package nlinux

import (
	"log"
	"os"
	"strings"
	"time"

	"github.com/itchio/itch-setup/cl"
)

type textUI struct {
	cli cl.CLI
}

var _ NativeUI = (*textUI)(nil)

type textInstallWindow struct {
	label     string
	progress  float64
	lastPrint time.Time
}

var printIncrement = 1 * time.Second

// NewTextUI creates a GTK3-based UI for the installer
func NewTextUI(cli cl.CLI) NativeUI {
	return &textUI{cli: cli}
}

func (u *textUI) Init() {
	// muffin to do
}

func (u *textUI) Main() {
	// just hang until os.Exit()
	var c chan struct{}
	<-c
}

func (u *textUI) CreateInstallWindow(baseTitle string) (NativeInstallWindow, error) {
	return &textInstallWindow{}, nil
}

func (u *textUI) ShowErrorAndQuit(err error) {
	log.Printf("Fatal error: %+v", err)
	os.Exit(1)
}

func (u *textUI) RunInMainThread(f func()) {
	// there's no threading conundrum with textUI,
	// we can just do it live
	f()
}

func (iw *textInstallWindow) SetTitle(title string) {
	log.Printf(title)
}
func (iw *textInstallWindow) SetLabel(label string) {
	iw.label = label
	iw.print()
}

func (iw *textInstallWindow) SetProgress(progress float64) {
	iw.progress = progress
	iw.print()
}

func (iw *textInstallWindow) print() {
	if time.Since(iw.lastPrint) > printIncrement {
		iw.lastPrint = time.Now()

		barWidth := 10
		sharps := int(iw.progress * float64(barWidth))
		dots := barWidth - sharps
		bar := strings.Repeat("#", sharps) + strings.Repeat(".", dots)

		log.Printf("[%s] %s", bar, iw.label)
	}
}
