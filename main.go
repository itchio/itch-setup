package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/go-errors/errors"

	"github.com/kardianos/osext"
	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

var (
	version       = "head" // set by command-line on CI release builds
	builtAt       = ""     // set by command-line on CI release builds
	commit        = ""     // set by command-line on CI release builds
	versionString = ""     // formatted on boot from 'version' and 'builtAt'
	appName       = "itch" // autodetected from executable name
	app           = kingpin.New("itchSetup", "The itch installer and self-updater")
)

func must(err error) {
	if err != nil {
		switch err := err.(type) {
		case *errors.Error:
			log.Fatal(err.ErrorStack())
		default:
			log.Fatal(err.Error())
		}
	}
}

func detectAppName() {
	execPath, err := osext.Executable()
	must(err)

	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	kitchBinary := fmt.Sprintf("kitchSetup%s", ext)

	if filepath.Base(execPath) == kitchBinary {
		appName = "kitch"
	}

	log.Println(appName, "setup starting up...")

	app.Name = fmt.Sprintf("%sSetup", appName)
}

func main() {
	detectAppName()
	app.UsageTemplate(kingpin.CompactUsageTemplate)

	app.HelpFlag.Short('h')
	if builtAt != "" {
		epoch, err := strconv.ParseInt(builtAt, 10, 64)
		must(err)
		versionString = fmt.Sprintf("%s, built on %s", version, time.Unix(epoch, 0).Format("Jan _2 2006 @ 15:04:05"))
	} else {
		versionString = fmt.Sprintf("%s, no build date", version)
	}

	app.Version(versionString)
	app.VersionFlag.Short('V')
	app.Author("Amos Wenger <amos@itch.io>")

	_, err := app.Parse(os.Args[1:])
	must(err)

	SetupMain()
}
