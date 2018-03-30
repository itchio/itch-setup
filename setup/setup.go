package setup

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	humanize "github.com/dustin/go-humanize"
	"github.com/itchio/itchSetup/localize"
	"github.com/itchio/wharf/archiver"
	"github.com/itchio/wharf/eos"
	"github.com/itchio/wharf/state"
	loghttp "github.com/motemen/go-loghttp"
)

type ErrorHandler func(message string)
type ProgressLabelHandler func(label string)
type ProgressHandler func(progress float64)
type FinishHandler func()
type SourceHandler func(source InstallSource)

type InstallerSettings struct {
	AppName         string
	Localizer       *localize.Localizer
	OnError         ErrorHandler
	OnProgressLabel ProgressLabelHandler
	OnProgress      ProgressHandler
	OnFinish        FinishHandler
	OnSource        SourceHandler
}

type Installer struct {
	settings   InstallerSettings
	gameID     int64
	sourceChan chan InstallSource
}

type InstallSource struct {
	Version string
	Archive eos.File
}

const brothBaseURL = "https://broth.itch.ovh"

func NewInstaller(settings InstallerSettings) *Installer {
	i := &Installer{
		settings:   settings,
		sourceChan: make(chan InstallSource),
	}
	go func() {
		err := i.warmUp()
		if err != nil {
			log.Printf("Install error: %s", err.Error())
			settings.OnError(err.Error())
		}
	}()
	return i
}

func (i *Installer) warmUp() error {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	if goos == "windows" {
		// TODO: remove me when we ship the first amd64 build
		goarch = "386"
	}

	channel := fmt.Sprintf("%s-%s", goos, goarch)

	client := http.Client{
		Transport: &loghttp.Transport{},
	}

	baseURL := fmt.Sprintf("%s/%s/%s", brothBaseURL, i.settings.AppName, channel)

	latestURL := fmt.Sprintf("%s/LATEST", baseURL)
	latestRes, err := client.Get(latestURL)
	if err != nil {
		return fmt.Errorf("While looking for latest version: %s", err.Error())
	}

	versionBytes, err := ioutil.ReadAll(latestRes.Body)
	if err != nil {
		return fmt.Errorf("While reading latest version: %s", err.Error())
	}

	version := strings.TrimSpace(string(versionBytes))

	log.Printf("Will install version %s\n", version)

	envVersion := os.Getenv("ITCHSETUP_VERSION")
	if envVersion != "" {
		log.Printf("Version overriden by environment: %s", envVersion)
		version = envVersion
	}

	archiveURL := fmt.Sprintf("%s/%s/.zip", baseURL, version)

	archive, err := eos.Open(archiveURL)
	if err != nil {
		return fmt.Errorf("While starting download: %s", err.Error())
	}

	source := InstallSource{
		Version: version,
		Archive: archive,
	}
	if i.settings.OnSource != nil {
		i.settings.OnSource(source)
	}
	i.sourceChan <- source
	return nil
}

func (i *Installer) Install(installDir string) {
	go func() {
		err := i.doInstall(installDir)
		if err != nil {
			i.settings.OnError(err.Error())
		} else {
			i.settings.OnFinish()
		}
	}()
}

func (i *Installer) doInstall(installDir string) error {
	localizer := i.settings.Localizer

	i.settings.OnProgressLabel(localizer.T("setup.status.preparing"))

	installSource := <-i.sourceChan
	archive := installSource.Archive

	stats, err := archive.Stat()
	if err != nil {
		return err
	}

	var uncompressedSize int64
	startTime := time.Now()

	consumer := &state.Consumer{
		OnProgress: func(progress float64) {
			percent := int(progress * 100.0)
			doneSize := int64(float64(uncompressedSize) * progress)
			secsSinceStart := time.Since(startTime).Seconds()
			donePerSec := int64(float64(doneSize) / float64(secsSinceStart))

			percentStr := fmt.Sprintf("%d%%", percent)
			speedStr := fmt.Sprintf("%s/s", humanize.IBytes(uint64(donePerSec)))

			progressLabel := fmt.Sprintf("%s - %s",
				localizer.T("setup.status.progress", map[string]string{"percent": percentStr}),
				localizer.T("setup.status.installing", map[string]string{"speed": speedStr}),
			)
			i.settings.OnProgressLabel(progressLabel)
			i.settings.OnProgress(progress)
		},
	}

	log.Printf("Installing to %s\n", installDir)

	xSettings := archiver.ExtractSettings{
		Consumer: consumer,
		OnUncompressedSizeKnown: func(size int64) {
			uncompressedSize = size
		},
	}
	_, err = archiver.ExtractZip(archive, stats.Size(), installDir, xSettings)
	if err != nil {
		return fmt.Errorf("Error while installing: %s", err.Error())
	}

	i.settings.OnProgressLabel(localizer.T("setup.status.done"))
	return nil
}
