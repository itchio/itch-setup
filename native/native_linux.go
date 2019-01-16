package native

import (
	"C"
	"bytes"
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"io/ioutil"

	"context"
	"strings"
	"sync"
	"time"

	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	"github.com/itchio/itch-setup/bindata"
	"github.com/itchio/itch-setup/cl"
	"github.com/itchio/itch-setup/setup"
	"github.com/pkg/errors"
)

var parentWin *gtk.Window

type nativeCore struct {
	cli     cl.CLI
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

	return nc, nil
}

var gtkOnce sync.Once

func initGtkOnce() {
	gtkOnce.Do(func() {
		gtk.Init(nil)
	})
}

func (nc *nativeCore) Install() error {
	var err error
	cli := nc.cli

	initGtkOnce()

	mv, err := setup.NewMultiverse(&setup.MultiverseParams{
		AppName: cli.AppName,
		BaseDir: nc.baseDir,
	})
	if err != nil {
		nc.ErrorDialog(errors.WithMessage(err, "Internal error"))
	}

	if cli.PreferLaunch {
		log.Printf("Launch preferred, attempting...")
		err := nc.tryLaunchCurrent(mv)
		if err != nil {
			log.Printf("While launching current: %+v", err)
			log.Printf("Continuing with setup...")
		}
	}

	log.Printf("Initializing installer GUI...")

	// Create a new toplevel window, set its title, and connect it to the
	// "destroy" signal to exit the GTK main loop when it is destroyed.
	win, err := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
	if err != nil {
		log.Fatal("Unable to create window:", err)
	}
	baseTitle := cli.Localizer.T("setup.window.title", map[string]string{"app_name": cli.AppName})
	win.SetTitle(baseTitle)
	win.Connect("destroy", func() {
		gtk.MainQuit()
	})

	box, err := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 18)
	if err != nil {
		log.Fatal("Unable to create box:", err)
	}
	win.Add(box)

	log.Printf("Loading image resources...")

	tmpDir, err := ioutil.TempDir("", "itch-setup-images")
	if err != nil {
		log.Fatal("Couldn't grab temp dir:", err)
	}

	err = os.MkdirAll(tmpDir, 0755)
	if err != nil {
		log.Fatal("Couldn't make temp dir:", err)
	}
	defer os.RemoveAll(tmpDir)

	loadBundledImage := func(path string) string {
		imageBytes, err := bindata.Asset(path)
		if err != nil {
			log.Fatal("Couldn't load image:", err)
		}

		imagePath := filepath.Join(tmpDir, filepath.Base(path))
		err = ioutil.WriteFile(imagePath, imageBytes, 0644)
		if err != nil {
			log.Fatal("Couldn't write image to temp dir:", err)
		}

		return imagePath
	}

	imagePath := loadBundledImage(fmt.Sprintf("data/installer-%s.png", cli.AppName))

	i, err := gtk.ImageNewFromFile(imagePath)
	if err != nil {
		log.Fatal("Unable to create image:", err)
	}
	box.Add(i)

	iconPath := loadBundledImage(fmt.Sprintf("data/%s-icon.png", cli.AppName))
	win.SetIconFromFile(iconPath)

	log.Printf("Setting up progress bar...")

	pb, err := gtk.ProgressBarNew()
	if err != nil {
		log.Fatal("Unable to create progress bar:", err)
	}
	pb.SetMarginStart(30)
	pb.SetMarginEnd(30)
	box.Add(pb)

	l, err := gtk.LabelNew("Warming up...")
	if err != nil {
		log.Fatal("Unable to create label:", err)
	}
	box.Add(l)

	vh, err := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 10)
	if err != nil {
		log.Fatal("Unable to create box:", err)
	}
	box.Add(vh)

	log.Printf("Positioning and showing window...")

	win.SetResizable(false)
	win.SetPosition(gtk.WIN_POS_CENTER)

	// Recursively show all widgets contained in this window.
	win.ShowAll()
	parentWin = win

	installer := setup.NewInstaller(setup.InstallerSettings{
		Localizer: cli.Localizer,
		AppName:   cli.AppName,
		OnProgress: func(progress float64) {
			glib.IdleAdd(func() {
				pb.SetFraction(progress)
			})
		},
		OnProgressLabel: func(label string) {
			glib.IdleAdd(func() {
				l.SetText(label)
			})
		},
		OnError: func(err error) {
			glib.IdleAdd(func() {
				nc.ErrorDialog(errors.WithMessage(err, "Warm-up error"))
			})
		},
		OnSource: func(source setup.InstallSource) {
			win.SetTitle(fmt.Sprintf("%s - %s", baseTitle, source.Version))
		},
		OnFinish: func(source setup.InstallSource) {
			glib.IdleAdd(func() {
				err := nc.installDesktopFiles()
				if err != nil {
					nc.ErrorDialog(err)
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
			glib.IdleAdd(func() {
				nc.ErrorDialog(errors.WithMessage(err, "Install error"))
			})
		}
	}

	go kickoffInstall()

	// Begin executing the GTK main loop.  This blocks until
	// gtk.MainQuit() is run.
	gtk.Main()

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
		Localizer: cli.Localizer,
		AppName:   cli.AppName,
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
		return errors.Errorf("No valid version of %s found installed", nc.cli.AppName)
	}

	log.Printf("Launching (%s) from (%s)", b.Version, b.Path)
	exePath := filepath.Join(b.Path, nc.exeName())

	cmd := exec.Command(exePath, nc.cli.Args...)

	err := cmd.Start()
	if err != nil {
		err = errors.WithMessage(err, fmt.Sprintf("Encountered a problem while launching %s", nc.cli.AppName))
		nc.ErrorDialog(err)
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
	cli := nc.cli
	initGtkOnce()

	msg := fmt.Sprintf("%+v", err)
	log.Printf("Fatal error: %s", msg)

	dialog := gtk.MessageDialogNewWithMarkup(parentWin, gtk.DIALOG_MODAL, gtk.MESSAGE_ERROR, gtk.BUTTONS_OK, "")
	buf := new(bytes.Buffer)
	fmt.Fprintf(buf, `<b>%s</b>`, cli.Localizer.T("setup.error_dialog.title"))
	buf.WriteString("\n\n")
	fmt.Fprintf(buf, `<i>%s-setup, %s</i>`, cli.AppName, cli.VersionString)
	buf.WriteString("\n\n")
	buf.WriteString(`<a href='https://github.com/itchio/itch/issues'>Open issue tracker</a>`)
	buf.WriteString("\n\n")
	xml.EscapeText(buf, []byte(msg))

	dialog.SetMarkup(buf.String())
	dialog.Connect("destroy", func() {
		gtk.MainQuit()
	})
	dialog.Connect("response", func() {
		gtk.MainQuit()
	})
	dialog.ShowAll()

	gtk.Main()
	os.Exit(1)
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
			return errors.WithMessage(err, "while updating desktop database")
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
	return ioutil.WriteFile(path, contents, perm)
}

func (nc *nativeCore) interpolate(source string, vars map[string]string) (string, error) {
	res := source
	for k, v := range vars {
		res = strings.Replace(res, "{{"+k+"}}", v, -1)
	}

	if strings.Contains(res, "{{") || strings.Contains(res, "}}") {
		return "", errors.Errorf("internal error: not fully interpolated:\n%s", res)
	}

	return res, nil
}

func (nc *nativeCore) installDesktopFiles() error {
	appName := nc.cli.AppName

	log.Printf("Determining whether or not we've been installed via an OS package...")

	execPath, err := os.Executable()
	if err != nil {
		return errors.WithMessage(err, "while getting self path")
	}

	if filepath.HasPrefix(execPath, "/usr") {
		log.Printf("Our execPath (%s) is somewhere in /usr, not installing desktop files", execPath)
		return nil
	}

	targetExecPath, err := CopySelf(filepath.Join(nc.baseDir, "itch-setup"))
	if err != nil {
		return errors.WithMessage(err, "while creating copy of self in install folder")
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
		return errors.WithMessage(err, "creating launch script")
	}

	iconPath := filepath.Join(nc.baseDir, "icon.png")

	imageData, err := bindata.Asset(fmt.Sprintf("data/%s-icon.png", appName))
	if err != nil {
		return errors.WithMessage(err, "while reading icon")
	}
	err = nc.writeFile(iconPath, imageData, 0644)
	if err != nil {
		return errors.WithMessage(err, "while writing icon")
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
		return errors.WithMessage(err, "writing desktop file")
	}

	err = nc.updateDesktopDatabase()
	if err != nil {
		return err
	}

	return nil
}
