package nlinux

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	"github.com/itchio/itch-setup/cl"
	"github.com/itchio/itch-setup/data"
)

// gtk UI implementation

type gtkUI struct {
	cli cl.CLI
	iw  *gtkInstallWindow
}

var _ NativeUI = (*gtkUI)(nil)

type gtkInstallWindow struct {
	cli cl.CLI
	win *gtk.Window
	l   *gtk.Label
	pb  *gtk.ProgressBar
}

var _ NativeInstallWindow = (*gtkInstallWindow)(nil)

// NewGtkUI creates a GTK3-based UI for the installer
func NewGtkUI(cli cl.CLI) NativeUI {
	return &gtkUI{cli: cli}
}

var gtkOnce sync.Once

func (u *gtkUI) Init() {
	gtkOnce.Do(func() {
		gtk.Init(nil)
	})
}

func (u *gtkUI) CreateInstallWindow(baseTitle string) (NativeInstallWindow, error) {
	win := &gtkInstallWindow{cli: u.cli}
	err := win.CreateAndShow(baseTitle)
	if err != nil {
		return nil, err
	}
	u.iw = win
	return win, nil
}

func (u *gtkUI) Main() {
	gtk.Main()
}

func (u *gtkUI) RunInMainThread(f func()) {
	glib.IdleAdd(f)
}

func (u *gtkUI) ShowErrorAndQuit(err error) {
	cli := u.cli
	u.Init()

	msg := fmt.Sprintf("%v", err)
	log.Printf("Fatal error: %s", msg)

	var parentWin *gtk.Window
	if u.iw != nil {
		parentWin = u.iw.win
	}
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

func (w *gtkInstallWindow) CreateAndShow(baseTitle string) error {
	var err error
	cli := w.cli

	// Create a new toplevel window, set its title, and connect it to the
	// "destroy" signal to exit the GTK main loop when it is destroyed.
	w.win, err = gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
	if err != nil {
		return fmt.Errorf("create GTK window: %w", err)
	}
	w.win.SetTitle(baseTitle)
	w.win.Connect("destroy", func() {
		gtk.MainQuit()
	})

	box, err := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 18)
	if err != nil {
		log.Fatal("Unable to create box:", err)
	}
	w.win.Add(box)

	log.Printf("Loading image resources...")

	tmpDir, err := os.MkdirTemp("", "itch-setup-images")
	if err != nil {
		return fmt.Errorf("create temp dir for images: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	loadBundledImage := func(path string) string {
		imageBytes, err := data.Asset(path)
		if err != nil {
			log.Fatal("Couldn't load image:", err)
		}

		imagePath := filepath.Join(tmpDir, filepath.Base(path))
		err = os.WriteFile(imagePath, imageBytes, 0644)
		if err != nil {
			log.Fatal("Couldn't write image to temp dir:", err)
		}

		return imagePath
	}

	imagePath := loadBundledImage(fmt.Sprintf("data/installer-%s.png", cli.AppName))

	i, err := gtk.ImageNewFromFile(imagePath)
	if err != nil {
		return fmt.Errorf("load installer image from %s: %w", imagePath, err)
	}
	box.Add(i)

	iconPath := loadBundledImage(fmt.Sprintf("data/%s-icon.png", cli.AppName))
	w.win.SetIconFromFile(iconPath)

	log.Printf("Setting up progress bar...")

	w.pb, err = gtk.ProgressBarNew()
	if err != nil {
		return fmt.Errorf("create progress bar: %w", err)
	}
	w.pb.SetMarginStart(30)
	w.pb.SetMarginEnd(30)
	box.Add(w.pb)

	w.l, err = gtk.LabelNew("Warming up...")
	if err != nil {
		log.Fatal("Unable to create label:", err)
	}
	box.Add(w.l)

	vh, err := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 10)
	if err != nil {
		return fmt.Errorf("create VBox: %w", err)
	}
	box.Add(vh)

	log.Printf("Positioning and showing window...")

	w.win.SetResizable(false)
	w.win.SetPosition(gtk.WIN_POS_CENTER)

	// Recursively show all widgets contained in this window.
	w.win.ShowAll()

	return nil
}

func (w *gtkInstallWindow) SetTitle(title string) {
	glib.IdleAdd(func() {
		w.win.SetTitle(title)
	})
}

func (w *gtkInstallWindow) SetLabel(label string) {
	glib.IdleAdd(func() {
		w.l.SetText(label)
	})
}

func (w *gtkInstallWindow) SetProgress(progress float64) {
	glib.IdleAdd(func() {
		w.pb.SetFraction(progress)
	})
}
