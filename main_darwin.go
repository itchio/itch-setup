package main

/*
int StartApp(void);
void SetLabel(char *cString);
void SetProgress(int value);
char *ValidateBundle(char *bundlePath);
void Relaunch(char *bundlePath);
void Quit();
*/
import "C"

import (
	"github.com/itchio/itchSetup/setup"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

func main() {
	C.StartApp()
}

//export StartItchSetup
func StartItchSetup() {
	var installer *setup.Installer

	tmpDir, err := ioutil.TempDir("", "itchSetup")
	if err != nil {
		log.Fatal("Could not get temporary directory", err)
	}

	trash := filepath.Join(tmpDir, "trash")

	err = os.MkdirAll(trash, os.FileMode(0755))
	if err != nil {
		log.Fatal("Could not create temporary directory", err)
	}

	installDir := filepath.Join(tmpDir, "itch.app")

	installer = setup.NewInstaller(setup.InstallerSettings{
		OnError: func(message string) {
			C.SetLabel(C.CString(message))
		},
		OnFinish: func() {
			target := "/Applications/itch.app"

			log.Printf("Validating new bundle...\n")
			errMsg := C.ValidateBundle(C.CString(installDir))
			if errMsg != nil {
				C.SetLabel(errMsg)
				return
			}
			log.Printf("New bundle is valid!\n")

			log.Printf("Setting up (re)launching %s\n", target)
			C.Relaunch(C.CString(target))

			os.Rename(target, filepath.Join(trash, "itch.app"))
			log.Printf("Trashing existing %s if any\n", target)

			log.Printf("Moving %s into %s\n", installDir, target)
			err := os.Rename(installDir, target)
			if err != nil {
				C.SetLabel(C.CString(err.Error()))
				return
			}

			log.Printf("Cleaning up %s\n", tmpDir)
			err = os.RemoveAll(tmpDir)
			if err != nil {
				C.SetLabel(C.CString(err.Error()))
				return
			}

			C.Quit()
		},
		OnProgress: func(progress float64) {
			C.SetProgress(C.int(progress * 1000.0))
		},
		OnProgressLabel: func(label string) {
			C.SetLabel(C.CString(label))
		},
	})

	installer.Install(installDir)
}
