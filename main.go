package main

import (
	"fmt"
	"log"
	"strings"
	"time"
	"unsafe"

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
	var inTE, outTE *walk.TextEdit
	var pb *walk.ProgressBar
	var progressLabel *walk.Label
	var mw *walk.MainWindow

	err := ui.MainWindow{
		Title: "itch Setup",
		MinSize: ui.Size{
			Width:  600,
			Height: 400,
		},
		Layout: ui.VBox{},
		Children: []ui.Widget{
			ui.HSplitter{
				Children: []ui.Widget{
					ui.TextEdit{AssignTo: &inTE},
					ui.TextEdit{AssignTo: &outTE, ReadOnly: true},
				},
			},
			ui.PushButton{
				Text: "SCREAM",
				OnClicked: func() {
					outTE.SetText(strings.ToUpper(inTE.Text()))
					dlg := new(walk.FileDialog)

					dlg.Title = "Select an install folder"

					if ok, err := dlg.ShowBrowseFolder(mw); err != nil {
						outTE.SetText(fmt.Sprintf("Error: %s", err.Error()))
					} else if !ok {
						outTE.SetText("Nothing picked")
					} else {
						outTE.SetText(fmt.Sprintf("Path picked: %s", dlg.FilePath))
					}
				},
			},
			ui.Label{
				AssignTo: &progressLabel,
			},
			ui.ProgressBar{
				AssignTo: &pb,
			},
		},
		AssignTo: &mw,
	}.Create()

	if err != nil {
		log.Fatal(err)
	}

	ic, err := walk.NewIconFromResourceId(101)
	if err != nil {
		log.Println("Could not load icon, oh well")
	} else {
		mw.SetIcon(ic)
	}

	centerWindow(mw)

	go func() {
		progress := 0
		for {
			progress = (progress + 1) % 100
			pb.SetValue(progress)
			time.Sleep(60 * time.Millisecond)
			progressLabel.SetText(fmt.Sprintf("Progress: %d%%", progress))
		}
	}()

	mw.Run()
}
