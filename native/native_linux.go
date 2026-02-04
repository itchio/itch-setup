package native

import (
	"C"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/itchio/ox/linox"

	"github.com/itchio/itch-setup/cl"
	"github.com/itchio/itch-setup/data"
	"github.com/itchio/itch-setup/native/nlinux"
	"github.com/itchio/itch-setup/setup"
)

type nativeCore struct {
	cli     cl.CLI
	nui     nlinux.NativeUI
	baseDir string
}

// NewCore returns a Linux-specific Core implementation
func NewCore(cli cl.CLI) (Core, error) {
	nc := &nativeCore{
		cli: cli,
	}

	// Linux policy: we default to `~/.itch` and `~/.kitch`
	// If you want it to point elsewhere, that's what symlinks are for!
	nc.baseDir = filepath.Join(os.Getenv("HOME"), fmt.Sprintf(".%s", cli.AppName))

	log.Printf("Initializing installer GUI...")
	if cli.Silent {
		log.Printf("Using text UI")
		nc.nui = nlinux.NewTextUI(cli)
	} else {
		log.Printf("Using GTK UI")
		nc.nui = nlinux.NewGtkUI(cli)
	}
	nc.nui.Init()

	return nc, nil
}

func (nc *nativeCore) Install() error {
	var err error
	cli := nc.cli

	mv, err := setup.NewMultiverse(&setup.MultiverseParams{
		AppName: cli.AppName,
		BaseDir: nc.baseDir,
	})
	if err != nil {
		return fmt.Errorf("Internal error: %w", err)
	}

	if cli.PreferLaunch {
		log.Printf("Launch preferred, attempting...")
		err := nc.tryLaunchCurrent(mv)
		if err != nil {
			log.Printf("While launching current: %+v", err)
			log.Printf("Continuing with setup...")
		}
	}

	baseTitle := cli.Localizer.T("setup.window.title", map[string]string{"app_name": cli.AppName})

	iw, err := nc.nui.CreateInstallWindow(baseTitle)
	if err != nil {
		return err
	}

	installer := setup.NewInstaller(setup.InstallerSettings{
		Localizer:  cli.Localizer,
		AppName:    cli.AppName,
		NoFallback: cli.NoFallback,
		OnProgress: func(progress float64) {
			iw.SetProgress(progress)
		},
		OnProgressLabel: func(label string) {
			iw.SetLabel(label)
		},
		OnError: func(err error) {
			nc.nui.RunInMainThread(func() {
				nc.nui.ShowErrorAndQuit(fmt.Errorf("Warm-up error: %w", err))
			})
		},
		OnSource: func(source setup.InstallSource) {
			iw.SetTitle(fmt.Sprintf("%s - %s", baseTitle, source.Version))
		},
		OnFinish: func(source setup.InstallSource) {
			nc.nui.RunInMainThread(func() {
				err := nc.installDesktopFiles()
				if err != nil {
					nc.ErrorDialog(err)
				}

				if nc.cli.Silent {
					log.Printf("Was silent installation, just quitting with successful exit code")
					os.Exit(0)
				}

				err = nc.tryLaunchCurrent(mv)
				if err != nil {
					nc.ErrorDialog(err)
				}
			})
		},
	})
	installer.WarmUp()

	kickoffInstall := func() {
		kickErr := func() error {
			installer.Install(mv)
			return nil
		}()
		if kickErr != nil {
			nc.nui.RunInMainThread(func() {
				nc.ErrorDialog(fmt.Errorf("Install error: %w", kickErr))
			})
		}
	}

	go kickoffInstall()

	nc.nui.Main()

	return nil
}

func (nc *nativeCore) Uninstall() error {
	warn := func(err error) {
		log.Printf("warning: %v", err)
		log.Printf("(continuing anyway)")
	}

	installedFiles := nc.installedFiles()
	for _, installedFile := range installedFiles {
		_, statErr := os.Lstat(installedFile)
		if statErr == nil {
			log.Printf("remove (%s)", installedFile)
			err := os.Remove(installedFile)
			if err != nil {
				warn(err)
			}
		}
	}

	cleanBaseDir := func() error {
		dir, err := os.Open(nc.baseDir)
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
			// application icon
			"icon.png": true,
			// copy of itch-setup
			"itch-setup": true,
			// installed version state
			"state.json": true,
			// launcher script
			nc.cli.AppName: true,
		}

		for _, name := range names {
			fullPath := filepath.Join(nc.baseDir, name)

			if deleteMap[name] {
				log.Printf("delete (%s)", fullPath)
				err := os.Remove(fullPath)
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

	err = nc.updateDesktopDatabase()
	if err != nil {
		warn(err)
	}

	// don't check result for that.
	// it may fail if the dir is not empty, which is fine
	os.Remove(nc.baseDir)

	log.Printf("%s is uninstalled.", nc.cli.AppName)
	log.Printf("")
	log.Printf("You might want to remove `~/.config/%s` as well: ", nc.cli.AppName)
	log.Printf("it contains your profile data and default install location.")
	log.Printf("")
	log.Printf("Have a nice day!")
	log.Printf("")

	return err
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
		err = nc.installDesktopFiles()
		if err != nil {
			return err
		}
	}
	return nil
}

func (nc *nativeCore) Relaunch() error {
	pid := nc.cli.RelaunchPID

	killIfExists := func() error {
		log.Printf("Finding PID (%d)...", pid)
		proc, err := os.FindProcess(pid)
		if err != nil {
			return err
		}
		log.Printf("Found PID %d, killing...", pid)
		err = proc.Kill()
		if err != nil {
			return err
		}
		log.Printf("Killed!")
		return nil
	}
	err := killIfExists()
	if err != nil {
		log.Printf("While killing: %+v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	setup.WaitForProcessToExit(ctx, pid)

	mv, err := nc.newMultiverse()
	if err != nil {
		return err
	}

	return nc.tryLaunchCurrent(mv)
}

func (nc *nativeCore) newMultiverse() (setup.Multiverse, error) {
	return setup.NewMultiverse(&setup.MultiverseParams{
		AppName: nc.cli.AppName,
		BaseDir: nc.baseDir,
	})
}

//

func (nc *nativeCore) tryLaunchCurrent(mv setup.Multiverse) error {
	if mv.HasReadyPending() {
		log.Printf("Has ready pending, trying to make it current...")
		err := mv.MakeReadyCurrent()
		if err != nil {
			log.Printf("Could not make ready current: %+v", err)
		}
	}

	b := mv.GetCurrentVersion()
	if b == nil {
		return fmt.Errorf("No valid version of %s found installed", nc.cli.AppName)
	}

	log.Printf("Launching (%s) from (%s)", b.Version, b.Path)
	exePath := filepath.Join(b.Path, nc.exeName())

	var args []string
	args = append(args, nc.cli.Args...)

	if linox.SupportsUnprivilegedCloneNewUser() {
		log.Printf("Kernel should support SUID sandboxing, leaving it enabled")
	} else {
		log.Printf("Kernel does *not* support unprivileged CLONE_USER, disabling SUID sandbox")
		args = append(args, "--no-sandbox")
	}

	cmd := exec.Command(exePath, args...)

	err := cmd.Start()
	if err != nil {
		nc.ErrorDialog(fmt.Errorf("Encountered a problem while launching %s: %w", nc.cli.AppName, err))
	}

	log.Printf("App launched, getting out of the way")
	os.Exit(0)

	// unreachable, but the go compiler doesn't know it
	return nil
}

func (nc *nativeCore) exeName() string {
	return nc.cli.AppName
}

func (nc *nativeCore) ErrorDialog(err error) {
	nc.nui.ShowErrorAndQuit(err)
	os.Exit(1) // just to be extra sure
}

// Typically `~/.local/share`
func (nc *nativeCore) xdgDataHome() string {
	xdgDataHome := os.Getenv("XDG_DATA_HOME")
	if xdgDataHome == "" {
		home := os.Getenv("HOME")
		xdgDataHome = filepath.Join(home, ".local", "share")
	}
	return xdgDataHome
}

// Typically `~/.local/share/applications`
func (nc *nativeCore) xdgAppDir() string {
	return filepath.Join(nc.xdgDataHome(), "applications")
}

// Typically `~/.local/share/applications/io.itch.kitch.desktop
func (nc *nativeCore) desktopFileName() string {
	desktopFileName := fmt.Sprintf("io.itch.%s.desktop", nc.cli.AppName)
	return filepath.Join(nc.xdgAppDir(), desktopFileName)
}

func (nc *nativeCore) installedFiles() []string {
	return []string{
		nc.desktopFileName(),
	}
}

func (nc *nativeCore) updateDesktopDatabase() error {
	log.Printf("Updating desktop database for (%s)", nc.xdgAppDir())
	{
		cmd := exec.Command("update-desktop-database", "-v", nc.xdgAppDir())
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			// don't hard fail here, cf. https://github.com/itchio/itch/issues/2289
			log.Printf("Warning: during update-desktop-database invocation: %s", err)
		}
	}
	return nil
}

func (nc *nativeCore) writeFile(path string, contents []byte, perm os.FileMode) error {
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		return err
	}

	log.Printf("install (%s)", path)
	return os.WriteFile(path, contents, perm)
}

func (nc *nativeCore) interpolate(source string, vars map[string]string) (string, error) {
	res := source
	for k, v := range vars {
		res = strings.ReplaceAll(res, "{{"+k+"}}", v)
	}

	if strings.Contains(res, "{{") || strings.Contains(res, "}}") {
		return "", fmt.Errorf("internal error: not fully interpolated:\n%s", res)
	}

	return res, nil
}

func (nc *nativeCore) installDesktopFiles() error {
	appName := nc.cli.AppName

	log.Printf("Determining whether or not we've been installed via an OS package...")

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("while getting self path: %w", err)
	}

	if filepath.HasPrefix(execPath, "/usr") {
		log.Printf("Our execPath (%s) is somewhere in /usr, not installing desktop files", execPath)
		return nil
	}

	targetExecPath, err := CopySelf(filepath.Join(nc.baseDir, "itch-setup"))
	if err != nil {
		return fmt.Errorf("while creating copy of self in install folder: %w", err)
	}

	launchScript := `#!/bin/sh
{{SETUPPATH}} --prefer-launch --appname {{APPNAME}} -- "$@"
`

	launchScript, err = nc.interpolate(launchScript, map[string]string{
		"SETUPPATH": targetExecPath,
		"APPNAME":   appName,
	})
	if err != nil {
		return err
	}

	launchDstPath := filepath.Join(nc.baseDir, appName)
	err = nc.writeFile(launchDstPath, []byte(launchScript), 0755)
	if err != nil {
		return fmt.Errorf("creating launch script: %w", err)
	}

	iconPath := filepath.Join(nc.baseDir, "icon.png")

	imageData, err := data.Asset(fmt.Sprintf("data/%s-icon.png", appName))
	if err != nil {
		return fmt.Errorf("while reading icon: %w", err)
	}
	err = nc.writeFile(iconPath, imageData, 0644)
	if err != nil {
		return fmt.Errorf("while writing icon: %w", err)
	}

	xdgAppDir := nc.xdgAppDir()
	desktopFileName := fmt.Sprintf("io.itch.%s.desktop", appName)
	desktopFilePath := filepath.Join(xdgAppDir, desktopFileName)

	desktopContents := `[Desktop Entry]
Type=Application
Name={{APPNAME}}
TryExec={{APPPATH}}
Exec={{APPPATH}} %U
Icon={{ICONPATH}}
Terminal=false
Categories=Game;
MimeType=x-scheme-handler/{{FIRST_PROTOCOL}};x-scheme-handler/{{SECOND_PROTOCOL}};
X-GNOME-Autostart-enabled=true
Comment=Install and play itch.io games easily`

	desktopContents, err = nc.interpolate(desktopContents, map[string]string{
		"APPNAME":         appName,
		"APPPATH":         launchDstPath,
		"ICONPATH":        iconPath,
		"FIRST_PROTOCOL":  appName + "io",
		"SECOND_PROTOCOL": appName,
	})
	if err != nil {
		return err
	}

	err = nc.writeFile(desktopFilePath, []byte(desktopContents), 0644)
	if err != nil {
		return fmt.Errorf("writing desktop file: %w", err)
	}

	err = nc.updateDesktopDatabase()
	if err != nil {
		return err
	}

	return nil
}

func (nc *nativeCore) Info() {
	log.Printf("nativeCore.Info() on Linux is a stub")
}
