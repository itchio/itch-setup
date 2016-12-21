package main

/*
int StartApp(void);
void SetLabel(char *cString);
void SetProgress(int value);
void Finish();
*/
import "C"

import (
	"github.com/fasterthanlime/itchSetup/setup"
)

func main() {
	C.StartApp()
}

//export StartItchSetup
func StartItchSetup() {
	var installer *setup.Installer

	installer = setup.NewInstaller(setup.InstallerSettings{
		OnError: func(message string) {
			C.SetLabel(C.CString(message))
		},
		OnFinish: func() {
			C.Finish()
		},
		OnProgress: func(progress float64) {
			C.SetProgress(C.int(progress * 1000.0))
		},
		OnProgressLabel: func(label string) {
			C.SetLabel(C.CString(label))
		},
	})

	installer.Install("/Applications/itch.app")
}
