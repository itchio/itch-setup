package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/itchio/itchSetup/setup"
	"github.com/kardianos/osext"
	"github.com/lxn/walk"
	ui "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

func rectangleFromRECT(r win.RECT) walk.Rectangle {
	return walk.Rectangle{
		X:      int(r.Left),
		Y:      int(r.Top),
		Width:  int(r.Right - r.Left),
		Height: int(r.Bottom - r.Top),
	}
}

func loadImage(filePath string) walk.Image {
	img, err := walk.NewImageFromFile(filePath)
	if err != nil {
		log.Printf("Couldn't load %s: %s\n", filePath, err.Error())
		return nil
	}
	return img
}

func centerWindow(mw *walk.FormBase) {
	// Center window
	var mi win.MONITORINFO
	mi.CbSize = uint32(unsafe.Sizeof(mi))

	if win.GetMonitorInfo(win.MonitorFromWindow(mw.Handle(), win.MONITOR_DEFAULTTOPRIMARY), &mi) {
		mon := rectangleFromRECT(mi.RcWork)
		mon.Height -= int(win.GetSystemMetrics(win.SM_CYCAPTION))

		size := mw.Size()

		mw.SetBounds(walk.Rectangle{
			X:      mon.X + (mon.Width-size.Width)/2,
			Y:      mon.Y + (mon.Height-size.Height)/2,
			Width:  size.Width,
			Height: size.Height,
		})
	}
}

func getUserDirectory(csidl win.CSIDL) (string, error) {
	localPathPtr := make([]uint16, 65536+2)
	var hwnd win.HWND
	success := win.SHGetSpecialFolderPath(hwnd, &localPathPtr[0], csidl, true)
	if !success {
		return "", errors.New("Could not get folder path")
	}
	return syscall.UTF16ToString(localPathPtr), nil
}

type ShortcutSettings struct {
	ShortcutFilePath string
	TargetPath       string
	Description      string
	IconLocation     string
	WorkingDirectory string
}

const windowsShortcutContent = `
	set WshShell = WScript.CreateObject("WScript.Shell")
	set shellLink = WshShell.CreateShortcut("%v")
	shellLink.TargetPath = "%v"
	shellLink.Description = "%v"
	shellLink.IconLocation = "%v"
	shellLink.WorkingDirectory = "%v"
	shellLink.Save`

func createShortcut(settings ShortcutSettings) error {
	shortcutScript := fmt.Sprintf(windowsShortcutContent,
		settings.ShortcutFilePath,
		settings.TargetPath,
		settings.Description,
		settings.IconLocation,
		settings.WorkingDirectory)

	tmpDir, err := ioutil.TempDir("", "itchSetupShortcut")
	if err != nil {
		return err
	}

	err = os.MkdirAll(tmpDir, 0755)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tmpPath := filepath.Join(tmpDir, "makeShortcut.vbs")
	err = ioutil.WriteFile(tmpPath, []byte(shortcutScript), 0644)
	if err != nil {
		return err
	}

	cmd := exec.Command("wscript", "/b", "/nologo", tmpPath)
	err = cmd.Run()
	if err != nil {
		return err
	}

	return nil
}

func SetupMain() {
	localPath, err := getUserDirectory(win.CSIDL_LOCAL_APPDATA)
	if err != nil {
		showError(err.Error(), nil)
		os.Exit(1)
	}

	roamingPath, err := getUserDirectory(win.CSIDL_APPDATA)
	if err != nil {
		showError(err.Error(), nil)
		os.Exit(1)
	}

	desktopPath, err := getUserDirectory(win.CSIDL_DESKTOP)
	if err != nil {
		showError(err.Error(), nil)
		os.Exit(1)
	}

	log.Println("AppData local path: ", localPath)
	log.Println("AppData roam' path: ", roamingPath)
	log.Println("Desktop path:       ", desktopPath)

	if *processStart != "" {
		log.Println("Should start itch, looking for versions")

		execFolder, err := osext.ExecutableFolder()
		if err != nil {
			log.Fatal(err)
		}

		entries, err := ioutil.ReadDir(execFolder)
		if err != nil {
			log.Printf("")
		}

		dirs := []string{}

		for _, entry := range entries {
			if !entry.IsDir() {
				log.Println("Skipping non-dir", entry.Name())
			}

			log.Println("Found dir", entry.Name())
			dirs = append(dirs, entry.Name())
		}

		sort.Strings(dirs)
		log.Printf("Sorted dir entries:\n%s", strings.Join(dirs, "\n"))

		if len(dirs) > 0 {
			first := dirs[0]
			cmd := exec.Command(first)
			cmd.Run()

			log.Printf("App quit")
			os.Exit(0)
		}
	}

	installDir := filepath.Join(localPath, "itch")

	err = createShortcut(ShortcutSettings{
		ShortcutFilePath: filepath.Join(desktopPath, "itch.lnk"),
		TargetPath:       filepath.Join(installDir, "itchSetup.exe") + " --processStart",
		Description:      "The best way to play your itch.io games",
		IconLocation:     filepath.Join(installDir, "app.ico"),
		WorkingDirectory: filepath.Join(installDir),
	})
	if err != nil {
		log.Println("While creating shortcut", err)
		showError(err.Error(), nil)
		os.Exit(1)
	}

	install(installDir)
}

func install(installDirIn string) {
	var installer *setup.Installer

	var ni *walk.NotifyIcon
	var installDirLabel *walk.LineEdit
	var pb *walk.ProgressBar
	var progressLabel *walk.Label
	var mw *walk.MainWindow
	var imageView *walk.ImageView
	var progressComposite, optionsComposite *walk.Composite

	installDir := installDirIn

	pickInstallLocation := func() {
		dlg := new(walk.FileDialog)

		dlg.Title = "Choose where the itch app should be installed"
		dlg.FilePath = installDir

		if ok, err := dlg.ShowBrowseFolder(mw); err != nil {
			log.Println(fmt.Sprintf("ShowBrowserFolder error: %s", err.Error()))
		} else if !ok {
			// nothing picked
		} else {
			installDir = dlg.FilePath
			installDirLabel.SetText(installDir)
		}
	}

	imageWidth := 622
	imageHeight := 301

	controlsHeight := 120
	windowHeight := imageHeight + 158 // found by trial & error

	windowSize := ui.Size{
		Width:  imageWidth,
		Height: windowHeight,
	}

	err := ui.MainWindow{
		Title:   "itch Setup",
		MinSize: windowSize,
		MaxSize: windowSize,
		Size:    windowSize,
		Layout: ui.VBox{
			MarginsZero: true,
			SpacingZero: true,
		},
		Children: []ui.Widget{
			ui.ImageView{
				AssignTo: &imageView,
				MinSize:  ui.Size{Width: imageWidth, Height: imageHeight},
				MaxSize:  ui.Size{Width: imageWidth, Height: imageHeight},
			},
			ui.Composite{
				MinSize: ui.Size{Height: controlsHeight},
				Layout: ui.VBox{
					Margins: ui.Margins{
						Left:  30,
						Right: 30,
					},
				},
				Children: []ui.Widget{
					ui.VSpacer{},
					ui.Label{
						Text: "Welcome to the itch installer! Grab a drink, pick an install location and proceed.",
					},
					ui.VSpacer{},
					ui.Composite{
						Layout: ui.HBox{
							MarginsZero: true,
						},
						Children: []ui.Widget{
							ui.LineEdit{
								AssignTo:    &installDirLabel,
								Text:        installDir,
								ReadOnly:    true,
								ToolTipText: "Click to change the install location",
								OnMouseUp: func(x, y int, button walk.MouseButton) {
									pickInstallLocation()
								},
							},
							ui.PushButton{
								MaxSize: ui.Size{Width: 1},
								Text:    "Install now",
								OnClicked: func() {
									progressComposite.SetVisible(true)
									optionsComposite.SetVisible(false)

									installer.Install(installDir)
								},
							},
						},
					},
					ui.VSpacer{},
				},
				AssignTo: &optionsComposite,
			},
			ui.Composite{
				MinSize: ui.Size{Height: controlsHeight},
				Layout: ui.VBox{
					Margins: ui.Margins{
						Left:  30,
						Right: 30,
					},
				},
				Children: []ui.Widget{
					ui.VSpacer{},
					ui.ProgressBar{
						MinValue: 0,
						MaxValue: 1000,
						AssignTo: &pb,
					},
					ui.VSpacer{Size: 10},
					ui.Label{
						Text:     "Starting installation...",
						AssignTo: &progressLabel,
					},
					ui.VSpacer{},
				},
				Visible:  false,
				AssignTo: &progressComposite,
			},
		},
		AssignTo: &mw,
		OnSizeChanged: func() {
			if mw == nil {
				return
			}
			// this is kinda crap UX, but resizing the window is really ugly
			mw.SetSize(walk.Size{Width: windowSize.Width, Height: windowSize.Height})
		},
	}.Create()

	// remove maximize button
	style := win.GetWindowLong(mw.Handle(), win.GWL_STYLE)
	style &^= win.WS_MAXIMIZEBOX
	// style &^= win.WS_THICKFRAME
	win.SetWindowLong(mw.Handle(), win.GWL_STYLE, style)

	if err != nil {
		log.Fatal(err)
	}

	ni, err = walk.NewNotifyIcon()
	if err != nil {
		log.Fatal(err)
	}

	// see itchSetup.rc
	ic, err := walk.NewIconFromResourceId(101)
	if err != nil {
		log.Println("Could not load icon, oh well")
	} else {
		ni.SetIcon(ic)
		mw.SetIcon(ic)
	}

	err = ni.SetVisible(true)
	if err != nil {
		log.Printf("Could not make notifyicon visible: %s", err.Error())
	}

	// thanks, go-bindata!
	func() {
		data, err := dataInstallerPngBytes()
		if err != nil {
			log.Printf("Installer image not found :()")
			return
		}

		tf, err := ioutil.TempFile("", "img")
		if err != nil {
			log.Printf("Could not create temp file for installer image")
			return
		}
		defer os.Remove(tf.Name())

		_, err = tf.Write(data)
		if err != nil {
			log.Printf("Could not write installer image to temp file")
			return
		}

		err = tf.Close()
		if err != nil {
			log.Printf("Could not finish writing installer image to temp file")
			return
		}

		img, err := walk.NewImageFromFile(tf.Name())
		if err != nil {
			log.Printf("Could not load installer image to temp file")
			return
		}

		imageView.SetImage(img)
	}()

	var source setup.InstallSource

	installer = setup.NewInstaller(setup.InstallerSettings{
		OnError: func(message string) {
			go showError(message, mw)
		},
		OnProgressLabel: func(label string) {
			progressLabel.SetText(label)
		},
		OnProgress: func(progress float64) {
			pb.SetValue(int(progress * 1000.0))
		},
		OnSource: func(sourceIn setup.InstallSource) {
			source = sourceIn
		},
		OnFinish: func() {
			err := CreateUninstallRegistryEntry(installDir, "itch", source.Version)
			if err != nil {
				log.Printf("While creating registry entry: %s", err.Error())
			}

			ni.ShowInfo("itch", "The installation went well, itch is now starting up!")

			itchPath := filepath.Join(installDir, "itch.exe")
			cmd := exec.Command(itchPath)
			err = cmd.Start()
			if err != nil {
				showError(err.Error(), mw)
			}

			time.Sleep(2 * time.Second)
			os.Exit(0)
		},
	})

	centerWindow(mw.AsFormBase())

	mw.Run()
}

func showError(errMsg string, mw *walk.MainWindow) {
	var dlg *walk.Dialog

	res, err := ui.Dialog{
		Title:    "Something went wrong",
		MinSize:  ui.Size{Width: 350},
		Layout:   ui.VBox{},
		AssignTo: &dlg,
		Children: []ui.Widget{
			ui.Composite{
				Layout: ui.HBox{
					MarginsZero: true,
				},
				Children: []ui.Widget{
					ui.Label{
						Text: errMsg,
					},
					ui.HSpacer{},
				},
			},
			ui.VSpacer{Size: 10},
			ui.Composite{
				Layout: ui.HBox{
					MarginsZero: true,
				},
				Children: []ui.Widget{
					ui.HSpacer{},
					ui.PushButton{
						Text:    "Okay :(",
						MaxSize: ui.Size{Width: 1},
						OnClicked: func() {
							dlg.Close(0)
						},
					},
					ui.HSpacer{},
				},
			},
		},
	}.Run(mw)

	centerWindow(dlg.AsFormBase())

	if err != nil {
		log.Printf("Error in dialog: %s\n", err.Error())
	}
	log.Printf("Dialog res: %#v\n", res)

	os.Exit(0)
}
