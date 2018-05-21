package native

/*
int StartApp(char *setupTitle, char *appName, char *imageBytes, int imageLen);
void SetLabel(char *cString);
void SetProgress(int value);
char *ValidateBundle(char *bundlePath);
int LaunchBundle(char *bundlePath);
void Quit();
*/
import "C"

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"github.com/itchio/itch-setup/bindata"
	"github.com/itchio/itch-setup/cl"
	"github.com/itchio/itch-setup/setup"
	"github.com/itchio/ox/macox"
	"github.com/pkg/errors"
)

type nativeCore struct {
	cli         cl.CLI
	selfPath    string
	roamingPath string
	homePath    string
}

var globalNc *nativeCore

func NewNativeCore(cli cl.CLI) (NativeCore, error) {
	nc := &nativeCore{
		cli: cli,
	}

	appSupportPath, err := macox.GetApplicationSupportPath()
	if err != nil {
		return nil, err
	}
	nc.roamingPath = filepath.Join(appSupportPath, cli.AppName)

	log.Printf("Roaming path: %s", nc.roamingPath)

	selfPath, err := os.Executable()
	if err != nil {
		return nil, err
	}
	nc.selfPath = selfPath
	log.Printf("Self path: %s", nc.selfPath)

	homePath, err := macox.GetHomeDirectory()
	if err != nil {
		return nil, err
	}
	nc.homePath = homePath
	log.Printf("Home path: %s", nc.homePath)

	globalNc = nc

	return nc, nil
}

func (nc *nativeCore) Install() error {
	cli := nc.cli
	setupTitle := cli.Localizer.T("setup.window.title", map[string]string{"app_name": cli.AppName})

	// thanks, go-bindata!
	imageData, err := bindata.Asset(fmt.Sprintf("data/installer-%s.png", cli.AppName))
	if err != nil {
		log.Printf("Installer image not found :()")
		return nil
	}

	imageBytes := unsafe.Pointer(&imageData[0])
	imageLen := C.int(len(imageData))
	C.StartApp(C.CString(setupTitle), C.CString(cli.AppName), (*C.char)(imageBytes), imageLen)
	return nil
}

func (nc *nativeCore) Uninstall() error {
	return errors.Errorf("uninstall: stub!")
}

func (nc *nativeCore) Upgrade() error {
	return errors.Errorf("upgrade: stub!")
}

func (nc *nativeCore) Relaunch() error {
	pid := nc.cli.RelaunchPID

	log.Printf("Should relaunch! Looking for PID %d...", pid)

	for tries := 10; tries > 0; tries-- {
		proc, err := os.FindProcess(pid)
		if err != nil {
			log.Printf("Waiting 2 seconds then retrying: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		proc.Release()
		break
	}

	bundlePath := nc.bundlePath()
	log.Printf("PID %d has exited, now launching bundle %s", pid, bundlePath)

	ret := C.LaunchBundle(C.CString(bundlePath))
	if int(ret) != 0 {
		log.Printf("Launched bundle successfully!")
	} else {
		log.Printf("Could not launch bundle successfully")
	}

	return nil
}

func (nc *nativeCore) ErrorDialog(err error) {
	// TODO: use cocoa for this?
	log.Fatalf("Fatal error: %+v", err)
}

//export StartItchSetup
func StartItchSetup() {
	var installer *setup.Installer
	nc := globalNc
	cli := nc.cli

	if cli.Silent {
		C.SetLabel(C.CString("Silent install mode is not supported on macOS"))
		return
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

	bundleName := nc.bundleName()
	installDir := filepath.Join(tmpDir, bundleName)

	installer = setup.NewInstaller(setup.InstallerSettings{
		Localizer: cli.Localizer,
		AppName:   cli.AppName,
		OnError: func(err error) {
			C.SetLabel(C.CString(fmt.Sprintf("%+v", err)))
		},
		OnFinish: func(source setup.InstallSource) {
			bundlePath := nc.bundlePath()

			log.Printf("Validating bundle for %s...\n", source.Version)
			errMsg := C.ValidateBundle(C.CString(installDir))
			if errMsg != nil {
				C.SetLabel(errMsg)
				return
			}
			log.Printf("New bundle is valid!\n")

			// always, even when installing kitch
			selfName := "itch-setup"
			selfDstPath := filepath.Join(nc.roamingPath, selfName)
			log.Printf("Copying self to %s", selfDstPath)

			showError := func(err error) {
				C.SetLabel(C.CString(fmt.Sprintf("%+v", err)))
			}

			selfSrc, err := os.Open(nc.selfPath)
			if err != nil {
				showError(err)
				return
			}
			defer selfSrc.Close()

			err = os.MkdirAll(filepath.Dir(selfDstPath), 0755)
			if err != nil {
				showError(err)
				return
			}

			selfDst, err := os.OpenFile(selfDstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
			if err != nil {
				showError(err)
				return
			}
			defer selfDst.Close()

			_, err = io.Copy(selfDst, selfSrc)
			if err != nil {
				showError(err)
				return
			}
			selfDst.Close()

			pidString := fmt.Sprintf("%d", os.Getpid())
			var args = []string{
				"--appname",
				cli.AppName,
				"--relaunch",
				"--relaunch-pid",
				pidString,
			}
			log.Printf("Starting %s ::: %s", selfDstPath, strings.Join(args, " ::: "))
			cmd := exec.Command(selfDstPath, args...)
			err = cmd.Start()
			if err != nil {
				showError(err)
				return
			}

			log.Printf("Trashing existing %s if any\n", bundlePath)
			os.Rename(bundlePath, filepath.Join(trash, bundleName))

			log.Printf("Moving %s into %s\n", installDir, bundlePath)
			err = os.Rename(installDir, bundlePath)
			if err != nil {
				showError(err)
				return
			}

			log.Printf("Cleaning up %s\n", tmpDir)
			err = os.RemoveAll(tmpDir)
			if err != nil {
				showError(err)
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
	installer.WarmUp()

	installer.Install(installDir)
}

func (nc *nativeCore) bundleName() string {
	return fmt.Sprintf("%s.app", nc.cli.AppName)
}

func (nc *nativeCore) bundlePath() string {
	return filepath.Join(nc.homePath, "Applications", nc.bundleName())
}
