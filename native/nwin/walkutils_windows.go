package nwin

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"unsafe"

	"github.com/itchio/itch-setup/bindata"
	"github.com/itchio/itch-setup/cl"
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

		size := mw.Size()

		mw.SetBounds(walk.Rectangle{
			X:      mon.X + (mon.Width-size.Width)/2,
			Y:      mon.Y + (mon.Height-size.Height)/2,
			Width:  size.Width,
			Height: size.Height,
		})
	}
}

func SetInstallerImage(cli cl.CLI, imageView *walk.ImageView) {
	// thanks, go-bindata!
	data, err := bindata.Asset(fmt.Sprintf("data/installer-%s.png", cli.AppName))
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
}
