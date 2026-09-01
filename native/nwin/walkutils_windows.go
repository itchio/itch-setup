package nwin

import (
	"bytes"
	"fmt"
	"image/png"
	"log"
	"unsafe"

	"github.com/itchio/itch-setup/cl"
	"github.com/itchio/itch-setup/data"
	"github.com/lxn/walk"
	"github.com/lxn/win"
)

func RectangleFromRECT(r win.RECT) walk.Rectangle {
	return walk.Rectangle{
		X:      int(r.Left),
		Y:      int(r.Top),
		Width:  int(r.Right - r.Left),
		Height: int(r.Bottom - r.Top),
	}
}

func LoadImage(filePath string) walk.Image {
	img, err := walk.NewImageFromFile(filePath)
	if err != nil {
		log.Printf("Couldn't load %s: %s\n", filePath, err.Error())
		return nil
	}
	return img
}

func CenterWindow(mw *walk.FormBase) {
	// Center window
	var mi win.MONITORINFO
	mi.CbSize = uint32(unsafe.Sizeof(mi))

	if win.GetMonitorInfo(win.MonitorFromWindow(mw.Handle(), win.MONITOR_DEFAULTTOPRIMARY), &mi) {
		mon := RectangleFromRECT(mi.RcWork)
		mon.Height -= int(win.GetSystemMetrics(win.SM_CYCAPTION))

		size := mw.SizePixels()

		mw.SetBoundsPixels(walk.Rectangle{
			X:      mon.X + (mon.Width-size.Width)/2,
			Y:      mon.Y + (mon.Height-size.Height)/2,
			Width:  size.Width,
			Height: size.Height,
		})
	}
}

// Pre-scaled variants of the installer image, see data/installer-*@*.png
var installerImageScales = []int{100, 150, 200}

func SetInstallerImage(cli cl.CLI, imageView *walk.ImageView) {
	dpi := imageView.DPI()
	wantScale := dpi * 100 / 96
	scale := installerImageScales[len(installerImageScales)-1]
	for _, s := range installerImageScales {
		if s >= wantScale {
			scale = s
			break
		}
	}

	name := fmt.Sprintf("data/installer-%s.png", cli.AppName)
	if scale != 100 {
		name = fmt.Sprintf("data/installer-%s@%d.png", cli.AppName, scale)
	}

	imageBytes, err := data.Asset(name)
	if err != nil {
		log.Printf("Installer image not found :()")
		return
	}

	src, err := png.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		log.Printf("Could not decode installer image: %s", err)
		return
	}

	img, err := walk.NewBitmapFromImageForDPI(src, scale*96/100)
	if err != nil {
		log.Printf("Could not create installer bitmap: %s", err)
		return
	}

	imageView.SetImage(img)
}
