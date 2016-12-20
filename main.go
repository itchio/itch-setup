package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unsafe"

	humanize "github.com/dustin/go-humanize"
	itchio "github.com/itchio/go-itchio"
	"github.com/itchio/go-itchio/itchfs"
	"github.com/itchio/wharf/archiver"
	"github.com/itchio/wharf/eos"
	"github.com/itchio/wharf/state"
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

// ItchSetupAPIKey belongs to a custom-made, empty itch.io account
const ItchSetupAPIKey = "sX3RL0lp73FZjmb19aEVcqHTuSbDuxT7id2QdZ93"

func main() {
	var ni *walk.NotifyIcon
	var installDirLabel *walk.LineEdit
	var pb *walk.ProgressBar
	var progressLabel *walk.Label
	var mw *walk.MainWindow
	var imageView *walk.ImageView

	installDir := filepath.Join(os.Getenv("LOCALAPPDATA"), "itch-experimental")

	var progressComposite, optionsComposite *walk.Composite

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

	install := func() {
		eos.RegisterHandler(&itchfs.ItchFS{
			ItchServer: "https://itch.io",
		})

		// game ID for fasterthanlime/itch
		const gameID int64 = 107034

		c := itchio.ClientWithKey(ItchSetupAPIKey)
		uploads, err := c.GameUploads(gameID)
		if err != nil {
			showError(err.Error())
			return
		}

		var upload *itchio.Upload
		for _, candidate := range uploads.Uploads {
			if candidate.ChannelName == "windows-32" {
				upload = candidate
				break
			}
		}

		if upload == nil {
			showError("No windows version found")
			return
		}

		if upload.Build == nil {
			showError("Windows version has no build")
			return
		}

		progressLabel.SetText(fmt.Sprintf("Downloading v%s", upload.Build.UserVersion))
		values := url.Values{}
		values.Set("api_key", c.Key)
		archiveURL := fmt.Sprintf("itchfs:///upload/%d/download/builds/%d/%s?%s",
			upload.ID, upload.Build.ID, "archive", values.Encode())

		archive, err := eos.Open(archiveURL)
		if err != nil {
			showError(err.Error())
			return
		}

		stats, err := archive.Stat()
		if err != nil {
			showError(err.Error())
			return
		}

		var uncompressedSize int64
		startTime := time.Now()

		consumer := &state.Consumer{
			OnProgress: func(progress float64) {
				percent := int(progress * 100.0)
				doneSize := int64(float64(uncompressedSize) * progress)
				secsSinceStart := time.Since(startTime).Seconds()
				donePerSec := int64(float64(doneSize) / float64(secsSinceStart))

				progressLabel.SetText(fmt.Sprintf("%d%% done - Downloading and installing @ %s/s",
					percent,
					humanize.IBytes(uint64(donePerSec)),
				))
				pb.SetValue(percent)
			},
		}

		progressLabel.SetText(fmt.Sprintf("Should download %s file", humanize.IBytes(uint64(stats.Size()))))

		xSettings := archiver.ExtractSettings{
			Consumer: consumer,
			OnUncompressedSizeKnown: func(size int64) {
				uncompressedSize = size
			},
		}
		_, err = archiver.ExtractZip(archive, stats.Size(), installDir, xSettings)
		if err != nil {
			showError(fmt.Sprintf("Error while installing: %s", err.Error()))
		}

		progressLabel.SetText("All done! Launching itch now...")
		ni.ShowInfo("itch", "The installation went well, itch is now starting up!")

		itchPath := filepath.Join(installDir, "itch.exe")
		cmd := exec.Command(itchPath)
		err = cmd.Start()
		if err != nil {
			go showError(err.Error())
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

									go install()
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

	err = ni.ShowInfo("Installing itch any time now!", "")
	if err != nil {
		log.Printf("Could not make notifyicon show info: %s", err.Error())
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

	centerWindow(mw)
	mw.Run()
}
