package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/go-errors/errors"
	"github.com/itchio/butler/comm"

	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

const appName = "itch"

var (
	version       = "head" // set by command-line on CI release builds
	builtAt       = ""     // set by command-line on CI release builds
	commit        = ""     // set by command-line on CI release builds
	versionString = ""     // formatted on boot from 'version' and 'builtAt'
	app           = kingpin.New("itchSetup", "The itch installer and self-updater")
	uninstall     = app.Flag("uninstall", "Uninstall the itch app").Bool()
)

func must(err error) {
	if err != nil {
		switch err := err.(type) {
		case *errors.Error:
			comm.Die(err.ErrorStack())
		default:
			comm.Die(err.Error())
		}
	}
}

func main() {
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
