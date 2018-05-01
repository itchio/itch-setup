package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"io/ioutil"

	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	"github.com/itchio/itch-setup/setup"
)

var parentWin *gtk.Window

func SetupMain() {
	var err error

	gtk.Init(nil)

	// Linux policy: we default to `~/.itch` and `~/.kitch`
	// If you want it to point elsewhere, that's what symlinks are for!
	baseDir := filepath.Join(os.Getenv("HOME"), fmt.Sprintf(".%s", appName))

	mv, err := setup.NewMultiverse(&setup.MultiverseParams{
		AppName: appName,
		BaseDir: baseDir,
	})
	if err != nil {
		showError("Internal error: %+v", err)
	}

	if cliParams.preferLaunch {
		log.Printf("Launch preferred, looking for a valid app dir...")
		if appDir, ok := mv.GetValidAppDir(); ok {
			tryLaunch(appDir)
			return
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
	win.SetTitle(localizer.T("setup.window.title", map[string]string{"app_name": appName}))
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
		imageBytes, err := Asset(path)
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

	imagePath := loadBundledImage("data/installer.png")

	i, err := gtk.ImageNewFromFile(imagePath)
	if err != nil {
		log.Fatal("Unable to create image:", err)
	}
	box.Add(i)

	win.SetIconFromFile(loadBundledImage("data/itch-icon.png"))

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

	versionInstallDir := ""
	sourceChan := make(chan setup.InstallSource, 1)

	installer := setup.NewInstaller(setup.InstallerSettings{
		Localizer: localizer,
		AppName:   appName,
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
				showError("Warm-up error: %+v", err)
			})
		},
		OnSource: func(source setup.InstallSource) {
			sourceChan <- source
		},
		OnFinish: func() {
			itchPath := filepath.Join(versionInstallDir, exeName())
			cmd := exec.Command(itchPath)
			err := cmd.Start()
			if err != nil {
				glib.IdleAdd(func() {
					l.SetText(err.Error())
				})
			}

			time.Sleep(2 * time.Second)
			gtk.MainQuit()
		},
	})

	kickoffInstall := func() {
		kickErr := func() error {
			source := <-sourceChan
			installer.Install(mv.GetAppDir(source.Version))

			return nil
		}()
		if kickErr != nil {
			glib.IdleAdd(func() {
				showError("Install error: %+v", kickErr)
			})
		}
	}

	go kickoffInstall()

	// Begin executing the GTK main loop.  This blocks until
	// gtk.MainQuit() is run.
	gtk.Main()
}

//

func tryLaunch(validAppDir string) {
	log.Println("Launching app")

	log.Printf("Via app dir: ", validAppDir)
	exePath := filepath.Join(validAppDir, exeName())

	cmd := exec.Command(exePath)

	err := cmd.Start()
	if err != nil {
		showError("Encountered a problem while launching %s: %s", appName, err.Error())
	}

	log.Printf("App launched, getting out of the way")
	os.Exit(0)
}

func showError(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	log.Printf("Fatal error: %s", msg)

	dialog := gtk.MessageDialogNewWithMarkup(parentWin, gtk.DIALOG_MODAL, gtk.MESSAGE_ERROR, gtk.BUTTONS_OK, "")
	buf := new(bytes.Buffer)
	fmt.Fprintf(buf, `<b>%s</b>`, localizer.T("setup.error_dialog.title"))
	buf.WriteString("\n\n")
	fmt.Fprintf(buf, `<i>%s-setup, %s</i>`, appName, versionString)
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

func exeName() string {
	return appName
}
