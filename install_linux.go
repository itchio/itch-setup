package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"io/ioutil"

	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
	"github.com/itchio/itch-setup/setup"
)

var installDir string

func SetupMain() {
	installDir := filepath.Join(os.Getenv("HOME"), fmt.Sprintf(".%s", appName))

	ids, err := assessInstallDirState(installDir)
	if err != nil {
		showError(fmt.Sprintf("%+v", err))
	}

	if ids.FoundMarker && len(ids.AppDirs) > 0 {
		log.Printf("Has marker and at least one app dir, trying to launch!")
		tryLaunch(ids)
		return
	}

	gtk.Init(nil)

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

	// Set the default window size.
	win.SetDefaultSize(imageWidth, imageHeight+260)

	win.SetPosition(gtk.WIN_POS_CENTER)

	// Recursively show all widgets contained in this window.
	win.ShowAll()

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
		OnError: func(message string) {
			glib.IdleAdd(func() {
				l.SetText(message)
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
			err := os.MkdirAll(installDir, 0755)
			if err != nil {
				return err
			}

			source := <-sourceChan

			versionInstallDir = filepath.Join(installDir, fmt.Sprintf("app-%s", source.Version))
			installer.Install(versionInstallDir)

			return nil
		}()
		if kickErr != nil {
			showError(kickErr.Error())
		}
	}

	go kickoffInstall()

	// Begin executing the GTK main loop.  This blocks until
	// gtk.MainQuit() is run.
	gtk.Main()
}

//

type InstallDirState struct {
	InstallDir    string
	FoundMarker   bool
	AppDirs       []string
	CurrentAppDir string
}

func assessInstallDirState(installDir string) (*InstallDirState, error) {
	res := &InstallDirState{
		InstallDir: installDir,
	}

	entries, err := ioutil.ReadDir(installDir)
	if err != nil {
		log.Printf("Empty (%s), that's ok", installDir)
		return res, nil
	}

	log.Printf("Looking through %d entries in %s", len(entries), installDir)
	for _, entry := range entries {
		if !entry.IsDir() {
			if entry.Name() == markerName() {
				log.Printf("Found marker...")
				res.FoundMarker = true
			}
			continue
		}

		if !strings.HasPrefix(entry.Name(), "app-") {
			continue
		}

		log.Printf("Found app dir %s", entry.Name())
		res.AppDirs = append(res.AppDirs, entry.Name())
	}

	if len(res.AppDirs) == 0 {
		log.Printf("No app dirs in sight, it's install time!")
		return res, nil
	}

	log.Printf("Found %d app dirs, sorting them...", len(res.AppDirs))
	sort.Strings(res.AppDirs)

	// make all paths absolute
	for i := range res.AppDirs {
		res.AppDirs[i] = filepath.Join(installDir, res.AppDirs[i])
	}
	res.CurrentAppDir = res.AppDirs[len(res.AppDirs)-1]

	return res, nil
}
func markerName() string {
	return fmt.Sprintf(".%s-marker", appName)
}

func exeName() string {
	return fmt.Sprintf("%s", appName)
}

func tryLaunch(ids *InstallDirState) {
	log.Println("Launching app")

	log.Printf("Current app dir: ", ids.CurrentAppDir)
	cmd := exec.Command(filepath.Join(ids.CurrentAppDir, exeName()))

	err := cmd.Start()
	if err != nil {
		showError(fmt.Sprintf("Encountered a problem while launching %s: %s", appName, err.Error()))
	}

	log.Printf("App launched, getting out of the way")
	os.Exit(0)
}

func showError(msg string) {
	log.Fatal(msg)
}
