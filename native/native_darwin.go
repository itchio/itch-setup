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
	"os/exec"
	"path/filepath"
	"strings"
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

func (nc *nativeCore) killRunningApp() {
	appName := nc.cli.AppName

	// Try graceful quit via AppleScript
	log.Printf("Attempting graceful quit of %s via AppleScript...", appName)
	cmd := exec.Command("osascript", "-e",
		fmt.Sprintf(`tell application "%s" to quit`, appName))
	cmd.Run()

	time.Sleep(2 * time.Second)

	// Force kill if still running
	log.Printf("Force killing any remaining %s.app processes...", appName)
	cmd = exec.Command("pkill", "-f", fmt.Sprintf("%s.app", appName))
	cmd.Run()
}

func (nc *nativeCore) Uninstall() error {
	warn := func(err error) {
		log.Printf("warning: %v", err)
		log.Printf("(continuing anyway)")
	}

	// Kill any running instances of the app
	nc.killRunningApp()

	// Remove app bundle from ~/Applications/
	appBundlePath := filepath.Join(nc.homeApplicationsPath, fmt.Sprintf("%s.app", nc.cli.AppName))
	_, statErr := os.Lstat(appBundlePath)
	if statErr == nil {
		log.Printf("remove (%s)", appBundlePath)
		err := os.RemoveAll(appBundlePath)
		if err != nil {
			warn(err)
		}
	}

	// Clean the base directory (~/Library/Application Support/{appname}-setup/)
	cleanBaseDir := func() error {
		dir, err := os.Open(nc.roamingSetupPath)
		if err != nil {
			if os.IsNotExist(err) {
				// good!
				return nil
			}
			return err
		}
		defer dir.Close()

		names, err := dir.Readdirnames(-1)
		if err != nil {
			return err
		}

		deleteMap := map[string]bool{
			// installed version state
			"state.json": true,
			// staging directory
			"staging": true,
		}

		for _, name := range names {
			fullPath := filepath.Join(nc.roamingSetupPath, name)

			if deleteMap[name] {
				log.Printf("delete (%s)", fullPath)
				err := os.RemoveAll(fullPath)
				if err != nil {
					warn(err)
				}
			} else if strings.HasPrefix(name, "app-") {
				log.Printf("delete (%s)/", fullPath)
				err := os.RemoveAll(fullPath)
				if err != nil {
					warn(err)
				}
			} else {
				log.Printf("keep (%s)", fullPath)
			}
		}
		return nil
	}

	err := cleanBaseDir()
	if err != nil {
		warn(err)
	}

	// Try to remove base directory (fails gracefully if not empty)
	os.Remove(nc.roamingSetupPath)

	// Clean app components from user data directory
	setup.CleanUserDataDir(nc.userDataPath(), warn)

	log.Printf("%s is uninstalled.", nc.cli.AppName)
	log.Printf("")
	log.Printf("Note: User data preserved in ~/Library/Application Support/%s", nc.cli.AppName)
	log.Printf("(contains: users/, preferences.json, config.json, db/)")
	log.Printf("")

	return nil
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

func (nc *nativeCore) userDataPath() string {
	appSupportPath, _ := macox.GetApplicationSupportPath()
	return filepath.Join(appSupportPath, nc.cli.AppName)
}

func (nc *nativeCore) Info() {
	log.Printf("nativeCore.Info() on Darwin is a stub")
}
