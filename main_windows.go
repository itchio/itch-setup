package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unsafe"

	"github.com/fasterthanlime/itchSetup/setup"
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

func centerWindow(mw *walk.MainWindow) {
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

func main() {
	var installer *setup.Installer

	var ni *walk.NotifyIcon
	var installDirLabel *walk.LineEdit
	var pb *walk.ProgressBar
	var progressLabel *walk.Label
	var mw *walk.MainWindow
	var imageView *walk.ImageView
	var progressComposite, optionsComposite *walk.Composite

	installDir := filepath.Join(os.Getenv("LOCALAPPDATA"), "itch-experimental")

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

	showError := func(errMsg string) {
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

		if err != nil {
			log.Printf("Error in dialog: %s\n", err.Error())
		}
		log.Printf("Dialog res: %#v\n", res)

		os.Exit(0)
	}

	onFinish := func() {
		ni.ShowInfo("itch", "The installation went well, itch is now starting up!")

		itchPath := filepath.Join(installDir, "itch.exe")
		cmd := exec.Command(itchPath)
		err := cmd.Start()
		if err != nil {
			showError(err.Error())
		}

		time.Sleep(2 * time.Second)
		os.Exit(0)
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

	installer = setup.NewInstaller(setup.InstallerSettings{
		OnError: func(message string) {
			go showError(message)
		},
		OnProgressLabel: func(label string) {
			progressLabel.SetText(label)
		},
		OnProgress: func(progress float64) {
			pb.SetValue(int(progress * 1000.0))
		},
		OnFinish: onFinish,
	})

	centerWindow(mw)
	mw.Run()
}
