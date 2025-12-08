package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Xuanwo/go-locale"
	"github.com/itchio/itch-setup/cl"
	"github.com/itchio/itch-setup/data"
	"github.com/itchio/itch-setup/localize"
	"github.com/itchio/itch-setup/native"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"

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

	app.Flag("info", "Just show info and quit").BoolVar(&cli.Info)
	app.Flag("relaunch", "Relaunch a new version of the itch app").BoolVar(&cli.Relaunch)
	app.Flag("relaunch-pid", "PID to wait for before relaunching").IntVar(&cli.RelaunchPID)

	app.Flag("appname", "Application name (itch or kitch)").StringVar(&cli.AppName)

	app.Flag("silent", "Run installation silently").BoolVar(&cli.Silent)

	app.Arg("args", "Arguments to pass down to itch (only supported on Linux & Windows)").StringsVar(&cli.Args)
}

func must(err error) {
	if err != nil {
		log.Fatalf("Fatal error: %+v", err)
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
	if runtime.GOOS == "darwin" {
		// this makes Cocoa very happy.
		runtime.LockOSThread()
	}

	logFileName := filepath.Join(os.TempDir(), "itch-setup-log.txt")
	log.Printf("itch-setup will log to %s", logFileName)
	logger := &lumberjack.Logger{
		Filename:   logFileName,
		MaxSize:    3, // megabytes
		MaxBackups: 3,
		MaxAge:     28, // days
	}
	log.SetOutput(io.MultiWriter(os.Stderr, logger))

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

	log.Printf("=========================================")
	log.Printf("itch-setup %q starting up at %q with arguments:", versionString, time.Now())
	for _, arg := range os.Args {
		log.Printf("%q", arg)
	}
	log.Printf("=========================================")

	app.Version(versionString)
	app.VersionFlag.Short('V')
	app.Author("Amos Wenger <amos@itch.io>")

	cli.VersionString = versionString

	var cliArgs []string

	// running in environments where we can't control command line arguments will
	// cause itch-setup to bail due to kingpin being strict about arugments. We
	// try to detect the environments and strip the args before parsing :/

	// detect macos app
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-psn") {
			// see https://github.com/itchio/itch-setup/issues/3
			log.Printf("Filtering out argument %q (passed by macOS when opened with Finder)", arg)
		} else {
			cliArgs = append(cliArgs, arg)
		}
	}

	// detect EGS, default to preferring launch instead of running setup
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-epicapp=") {
			cliArgs = []string{"--prefer-launch"}
			break
		}
	}

	_, err := app.Parse(cliArgs)
	must(err)

	detectAppName()

	userLocale := DefaultLocale
	tag, err := locale.Detect()
	if err != nil {
		log.Println("Couldn't detect locale, falling back to default", DefaultLocale)
	} else {
		userLocale = tag.String()
	}

	envLocale := os.Getenv("ITCH_SETUP_LOCALE")
	if envLocale != "" {
		log.Println("ITCH_SETUP_LOCALE set, switching to ", envLocale)
		userLocale = envLocale
	}

	log.Println("Locale: ", userLocale)

	localizer, err = localize.NewLocalizer(data.Asset)
	if err != nil {
		log.Fatal(err)
	}

	err = localizer.LoadLocale(userLocale)
	if err != nil {
		if len(userLocale) >= 2 {
			userLocale = userLocale[:2]
			err = localizer.LoadLocale(userLocale)
		} else {
			log.Println("Ignoring locale: ", userLocale)
		}
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
	if cli.Info {
		verbs = append(verbs, "info")
	}

	if len(verbs) > 1 {
		nc.ErrorDialog(fmt.Errorf("Cannot specify more than one verb: got %s", strings.Join(verbs, ", ")))
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
			jsonlBail(fmt.Errorf("Fatal upgrade error: %w", err))
		}
	case "relaunch":
		if cli.RelaunchPID <= 0 {
			jsonlBail(fmt.Errorf("--relaunch needs a valid --relaunch-pid (got %d)", cli.RelaunchPID))
		}

		err = nc.Relaunch()
		if err != nil {
			jsonlBail(fmt.Errorf("Fatal relaunch error: %w", err))
		}
	case "uninstall":
		err = nc.Uninstall()
		if err != nil {
			nc.ErrorDialog(err)
		}
	case "info":
		nc.Info()
	}
}

func jsonlBail(err error) {
	// TODO: use json-lines
	log.Fatalf("%+v", err)
}
