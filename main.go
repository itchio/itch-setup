package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/cloudfoundry-attic/jibber_jabber"
	"github.com/itchio/itch-setup/bindata"
	"github.com/itchio/itch-setup/cl"
	"github.com/itchio/itch-setup/localize"
	"github.com/itchio/itch-setup/native"
	"github.com/pkg/errors"

	kingpin "gopkg.in/alecthomas/kingpin.v2"
)

var (
	version       = "head" // set by command-line on CI release builds
	builtAt       = ""     // set by command-line on CI release builds
	commit        = ""     // set by command-line on CI release builds
	target        = ""     // set by command-line on CI release builds
	versionString = ""     // formatted on boot from 'version' and 'builtAt'
	app           = kingpin.New("itch-setup", "The itch installer and self-updater")
)

var cli cl.CLI

func init() {
	app.Flag("prefer-launch", "Launch if a valid version of itch is installed").BoolVar(&cli.PreferLaunch)

	app.Flag("upgrade", "Upgrade the itch app if necessary").BoolVar(&cli.Upgrade)

	app.Flag("uninstall", "Uninstall the itch app").BoolVar(&cli.Uninstall)

	app.Flag("relaunch", "Relaunch a new version of the itch app").BoolVar(&cli.Relaunch)
	app.Flag("relaunch-pid", "PID to wait for before relaunching").IntVar(&cli.RelaunchPID)

	app.Flag("appname", "Application name (itch or kitch)").StringVar(&cli.AppName)

	app.Flag("silent", "Run installation silently").BoolVar(&cli.Silent)

	app.Arg("args", "Arguments to pass down to itch (only supported on Linux & Windows)").StringsVar(&cli.Args)
}

func must(err error) {
	if err != nil {
		log.Fatalf("%+v", err)
	}
}

func detectAppName() {
	if cli.AppName != "" {
		log.Printf("App name specified on command-line: %s", cli.AppName)
	} else if target != "" {
		cli.AppName = strings.TrimSuffix(target, "-setup")
		log.Printf("App name specified at build time: %s", cli.AppName)
	} else {
		execPath, err := os.Executable()
		must(err)

		ext := ""
		if runtime.GOOS == "windows" {
			ext = ".exe"
		}
		kitchBinary := fmt.Sprintf("kitch-setup%s", ext)

		if filepath.Base(execPath) == kitchBinary {
			cli.AppName = "kitch"
		} else {
			cli.AppName = "itch"
		}
		log.Printf("App name detected: %s", cli.AppName)
	}

	app.Name = fmt.Sprintf("%s-setup", cli.AppName)
}

const DefaultLocale = "en-US"

var localizer *localize.Localizer

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
	if commit != "" {
		versionString = fmt.Sprintf("%s, ref %s", versionString, commit)
	}

	app.Version(versionString)
	app.VersionFlag.Short('V')
	app.Author("Amos Wenger <amos@itch.io>")

	cli.VersionString = versionString

	_, err := app.Parse(os.Args[1:])
	must(err)

	detectAppName()

	userLocale, err := jibber_jabber.DetectIETF()
	if err != nil {
		log.Println("Couldn't detect locale, falling back to default", DefaultLocale)
		userLocale = "en-US"
	}

	log.Println("Locale: ", userLocale)

	localizer, err = localize.NewLocalizer(bindata.Asset)
	if err != nil {
		log.Fatal(err)
	}

	err = localizer.LoadLocale(userLocale)
	if err != nil {
		userLocale = userLocale[:2]
		err = localizer.LoadLocale(userLocale)
	}

	if err == nil {
		localizer.SetLang(userLocale)
	}
	cli.Localizer = localizer

	nc, err := native.NewCore(cli)
	if err != nil {
		panic(err)
	}

	var verbs []string

	if cli.Upgrade {
		verbs = append(verbs, "upgrade")
	}
	if cli.Relaunch {
		verbs = append(verbs, "relaunch")
	}
	if cli.Uninstall {
		verbs = append(verbs, "uninstall")
	}

	if len(verbs) > 1 {
		nc.ErrorDialog(errors.Errorf("Cannot specify more than one verb: got %s", strings.Join(verbs, ", ")))
	}

	if len(verbs) == 0 {
		verbs = append(verbs, "install")
	}

	switch verbs[0] {
	case "install":
		err = nc.Install()
		if err != nil {
			nc.ErrorDialog(err)
		}
	case "upgrade":
		err = nc.Upgrade()
		if err != nil {
			jsonlBail(errors.WithMessage(err, "Fatal upgrade error"))
		}
	case "relaunch":
		if cli.RelaunchPID <= 0 {
			jsonlBail(errors.Errorf("--relaunch needs a valid --relaunch-pid (got %d)", cli.RelaunchPID))
		}

		err = nc.Relaunch()
		if err != nil {
			jsonlBail(errors.WithMessage(err, "Fatal relaunch error"))
		}
	case "uninstall":
		err = nc.Uninstall()
		if err != nil {
			nc.ErrorDialog(err)
		}
	}
}

func jsonlBail(err error) {
	// TODO: use json-lines
	log.Fatalf("%+v", err)
}
