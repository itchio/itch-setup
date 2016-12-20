package main

/*
int StartApp(void);
void SetLabel(char *cString);
void SetPercent(int percent);
*/
import "C"

import (
	"github.com/fasterthanlime/itchSetup/setup"
	"log"
	"os"
)

func main() {
	C.StartApp()
}

//export StartItchSetup
func StartItchSetup() {
	var installer *setup.Installer

	done := make(chan bool)

	installer = setup.NewInstaller(setup.InstallerSettings{
		OnError: func(message string) {
			log.Printf("Error: %s\n", message)
			done <- true
		},
		OnFinish: func() {
			log.Printf("All done!")
			done <- true
		},
		OnProgress: func(progress float64) {
			C.SetPercent(C.int(progress * 100.0))
		},
		OnProgressLabel: func(label string) {
			C.SetLabel(C.CString(label))
		},
	})

	installer.Install("/Applications/itch.app")
	go func() {
		done <- true
		os.Exit(0)
	}()
}
