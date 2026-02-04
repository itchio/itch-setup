package native

/*
int StartApp(char *setupTitle, char *appName, char *imageBytes, int imageLen);
void SetLabel(char *cString);
void SetProgress(int value);
void SetInstalling(int installing);
char *ValidateBundle(char *bundlePath);
int LaunchBundle(char *bundlePath);
void Quit();
*/
import "C"

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"github.com/itchio/itch-setup/cl"
	"github.com/itchio/itch-setup/data"
	"github.com/itchio/itch-setup/setup"
	"github.com/itchio/ox/macox"
)

type nativeCore struct {
	cli                  cl.CLI
	selfPath             string
	roamingSetupPath     string
	homeApplicationsPath string
}

var globalNc *nativeCore

// NewCore returns a macOS-specific Core implementation
func NewCore(cli cl.CLI) (Core, error) {
	nc := &nativeCore{
		cli: cli,
	}

	appSupportPath, err := macox.GetApplicationSupportPath()
	if err != nil {
		return nil, err
	}
	nc.roamingSetupPath = filepath.Join(appSupportPath, fmt.Sprintf("%s-setup", cli.AppName))

	log.Printf("Base dir: %s", nc.roamingSetupPath)

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
	nc.homeApplicationsPath = filepath.Join(homePath, "Applications")
	log.Printf("Home Applications path: %s", nc.homeApplicationsPath)

	globalNc = nc

	return nc, nil
}

func (nc *nativeCore) Install() error {
	cli := nc.cli
	setupTitle := cli.Localizer.T("setup.window.title", map[string]string{"app_name": cli.AppName})

	imageData, err := data.Asset(fmt.Sprintf("data/installer-%s.png", cli.AppName))
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
	return fmt.Errorf("uninstall: stub!")
}

func (nc *nativeCore) Upgrade() error {
	cli := nc.cli

	mv, err := nc.newMultiverse()
	if err != nil {
		return err
	}

	installer := setup.NewInstaller(setup.InstallerSettings{
		Localizer:  cli.Localizer,
		AppName:    cli.AppName,
		NoFallback: cli.NoFallback,
	})
	res, err := installer.Upgrade(mv)
	if err != nil {
		return err
	}

	if res.DidUpgrade {
		log.Printf("Did upgrade! But nothing to do about it on macOS.")
	}

	return nil
}

func (nc *nativeCore) Relaunch() error {
	pid := nc.cli.RelaunchPID

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	setup.WaitForProcessToExit(ctx, pid)

	mv, err := nc.newMultiverse()
	if err != nil {
		return err
	}

	if mv.HasReadyPending() {
		err = mv.MakeReadyCurrent()
		if err != nil {
			return err
		}
	}

	nc.tryLaunchCurrent(mv)
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

	mv, err := nc.newMultiverse()
	if err != nil {
		nc.ErrorDialog(err)
	}

	if cli.Silent {
		C.SetLabel(C.CString("Silent install mode is not supported on macOS"))
		return
	}

	if cli.PreferLaunch {
		log.Printf("--prefer-launch passed, looking for valid install")
		err := nc.tryLaunchCurrent(mv)
		if err != nil {
			log.Printf("Could not launch current: %v", err)
			log.Printf("Carrying on with install")
		}
	}

	installer = setup.NewInstaller(setup.InstallerSettings{
		Localizer:  cli.Localizer,
		AppName:    cli.AppName,
		NoFallback: cli.NoFallback,
		OnError: func(err error) {
			C.SetInstalling(0)
			log.Printf("Error: %+v", err)
			C.SetLabel(C.CString(fmt.Sprintf("%+v", err)))
		},
		OnFinish: func(source setup.InstallSource) {
			C.SetInstalling(0)
			err := nc.tryLaunchCurrent(mv)
			if err != nil {
				nc.ErrorDialog(err)
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

	C.SetInstalling(1)
	installer.Install(mv)
}

func (nc *nativeCore) tryLaunchCurrent(mv setup.Multiverse) error {
	b := mv.GetCurrentVersion()
	if b == nil {
		return fmt.Errorf("No valid version of %s found installed", nc.cli.AppName)
	}

	log.Printf("Launching (%s) from (%s)", b.Version, b.Path)
	if C.LaunchBundle(C.CString(b.Path)) == 0 {
		return fmt.Errorf("Could not launch (%s)", b.Path)
	}

	log.Printf("Bundle launched successfully, getting out of the way")
	C.Quit()

	// unreachable, but the go compiler doesn't know it
	return nil
}

func (nc *nativeCore) validateBundle(bundlePath string) error {
	log.Printf("Making sure (%s) is signed and valid", bundlePath)

	result := C.ValidateBundle(C.CString(bundlePath))
	if result != nil {
		return fmt.Errorf("Bundle (%s) invalid: %s", bundlePath, C.GoString(result))
	}
	return nil
}

func (nc *nativeCore) newMultiverse() (setup.Multiverse, error) {
	return setup.NewMultiverse(&setup.MultiverseParams{
		AppName:         nc.cli.AppName,
		BaseDir:         nc.roamingSetupPath,
		ApplicationsDir: nc.homeApplicationsPath,

		OnValidate: nc.validateBundle,
	})
}

func (nc *nativeCore) Info() {
	log.Printf("nativeCore.Info() on Darwin is a stub")
}
