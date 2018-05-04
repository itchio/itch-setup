package native

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

	"github.com/itchio/itch-setup/cl"
	"github.com/itchio/itch-setup/setup"
)

var cli cl.CLI

func Do(cliArg cl.CLI) {
	cli = cliArg
	setupTitle := cli.Localizer.T("setup.window.title", map[string]string{"app_name": cli.AppName})
	C.StartApp(C.CString(setupTitle), C.CString(cli.AppName))
}

//export StartItchSetup
func StartItchSetup() {
	var installer *setup.Installer

	if cli.PreferLaunch {
		log.Fatal("prefer launch passed, but don't know how to do that on macOS")
	}

	if cli.Uninstall {
		log.Fatal("uninstall passed, but don't know how to do that on macOS")
	}

	if cli.Relaunch {
		log.Fatal("relaunch passed, but don't know how to do that on macOS")
	}

	tmpDir, err := ioutil.TempDir("", fmt.Sprintf("%s-setup", cli.AppName))
	if err != nil {
		log.Fatal("Could not get temporary directory", err)
	}

	trash := filepath.Join(tmpDir, "trash")

	err = os.MkdirAll(trash, os.FileMode(0755))
	if err != nil {
		log.Fatal("Could not create temporary directory", err)
	}

	bundleName := fmt.Sprintf("%s.app", cli.AppName)
	installDir := filepath.Join(tmpDir, bundleName)

	installer = setup.NewInstaller(setup.InstallerSettings{
		Localizer: cli.Localizer,
		AppName:   cli.AppName,
		OnError: func(err error) {
			C.SetLabel(C.CString(fmt.Sprintf("%+v", err)))
		},
		OnFinish: func(source setup.InstallSource) {
			target := fmt.Sprintf("/Applications/%s", bundleName)

			log.Printf("Validating bundle for %s...\n", source.Version)
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
