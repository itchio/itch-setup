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

	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	"github.com/itchio/itch-setup/bindata"
	"github.com/itchio/itch-setup/cl"
	"github.com/itchio/itch-setup/setup"
	"github.com/pkg/errors"
)
import (
	"context"
	"sync"
	"time"
)

var parentWin *gtk.Window

type nativeCore struct {
	cli     cl.CLI
	baseDir string
}

func NewNativeCore(cli cl.CLI) (NativeCore, error) {
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

	imageWidth := 622
	imageHeight := 301

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

	// Set the default window size.
	win.SetDefaultSize(imageWidth, imageHeight+260)
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
				err := nc.tryLaunchCurrent(mv)
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
	return errors.Errorf("uninstall: stub!")
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
	return installer.Upgrade(mv)
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
