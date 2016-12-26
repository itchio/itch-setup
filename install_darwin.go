package main

/*
int StartApp(char *appName);
void SetLabel(char *cString);
void SetProgress(int value);
char *ValidateBundle(char *bundlePath);
void Relaunch(char *bundlePath);
void Quit();
*/
import "C"

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/itchio/itchSetup/setup"
)

func SetupMain() {
	C.StartApp(C.CString(appName))
}

//export StartItchSetup
func StartItchSetup() {
	var installer *setup.Installer

	tmpDir, err := ioutil.TempDir("", fmt.Sprintf("%sSetup", appName))
	if err != nil {
		log.Fatal("Could not get temporary directory", err)
	}

	trash := filepath.Join(tmpDir, "trash")

	err = os.MkdirAll(trash, os.FileMode(0755))
	if err != nil {
		log.Fatal("Could not create temporary directory", err)
	}

	bundleName := fmt.Sprintf("%s.app", appName)
	installDir := filepath.Join(tmpDir, bundleName)

	installer = setup.NewInstaller(setup.InstallerSettings{
		AppName: appName,
		OnError: func(message string) {
			C.SetLabel(C.CString(message))
		},
		OnFinish: func() {
			target := fmt.Sprintf("/Applications/%s", bundleName)

			log.Printf("Validating new bundle...\n")
			errMsg := C.ValidateBundle(C.CString(installDir))
			if errMsg != nil {
				C.SetLabel(errMsg)
				return
			}
			log.Printf("New bundle is valid!\n")

			log.Printf("Setting up (re)launching %s\n", target)
			C.Relaunch(C.CString(target))

			os.Rename(target, filepath.Join(trash, bundleName))
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
