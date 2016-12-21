package setup

import (
	"fmt"
	"log"
	"net/url"
	"runtime"
	"sync"
	"time"

	humanize "github.com/dustin/go-humanize"
	itchio "github.com/itchio/go-itchio"
	"github.com/itchio/go-itchio/itchfs"
	"github.com/itchio/wharf/archiver"
	"github.com/itchio/wharf/eos"
	"github.com/itchio/wharf/state"
)

// ItchSetupAPIKey belongs to a custom-made, empty itch.io account
const ItchSetupAPIKey = "sX3RL0lp73FZjmb19aEVcqHTuSbDuxT7id2QdZ93"

type ErrorHandler func(message string)
type ProgressLabelHandler func(label string)
type ProgressHandler func(progress float64)
type FinishHandler func()
type SourceHandler func(source InstallSource)

type InstallerSettings struct {
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

var once sync.Once

func NewInstaller(settings InstallerSettings) *Installer {
	once.Do(func() {
		eos.RegisterHandler(&itchfs.ItchFS{
			ItchServer: "https://itch.io",
		})
	})

	i := &Installer{
		settings: settings,
		// game ID for fasterthanlime/itch
		gameID:     107034,
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
	c := itchio.ClientWithKey(ItchSetupAPIKey)
	uploads, err := c.GameUploads(i.gameID)
	if err != nil {
		return fmt.Errorf("While listing uploads: %s", err.Error())
	}

	var channelName string
	if runtime.GOOS == "windows" {
		channelName = "windows-32"
	} else if runtime.GOOS == "darwin" {
		channelName = "mac-64"
	} else {
		if runtime.GOARCH == "386" {
			channelName = "linux-32"
		} else {
			channelName = "linux-64"
		}
	}

	var upload *itchio.Upload
	for _, candidate := range uploads.Uploads {
		if candidate.ChannelName == channelName {
			upload = candidate
			break
		}
	}

	if upload == nil {
		return fmt.Errorf("No %s version found", channelName)
	}

	if upload.Build == nil {
		return fmt.Errorf("%s version has no build", channelName)
	}

	log.Printf("Will install v%s\n", upload.Build.UserVersion)

	values := url.Values{}
	values.Set("api_key", c.Key)
	archiveURL := fmt.Sprintf("itchfs:///upload/%d/download/builds/%d/%s?%s",
		upload.ID, upload.Build.ID, "archive", values.Encode())

	archive, err := eos.Open(archiveURL)
	if err != nil {
		return fmt.Errorf("While starting download: %s", err.Error())
	}

	source := InstallSource{
		Version: upload.Build.UserVersion,
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

			progressLabel := fmt.Sprintf("%d%% done - Downloading and installing @ %s/s",
				percent,
				humanize.IBytes(uint64(donePerSec)),
			)
			i.settings.OnProgressLabel(progressLabel)
			i.settings.OnProgress(progress)
		},
	}

	i.settings.OnProgressLabel("Warming up...")

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

	i.settings.OnProgressLabel("All done!")
	return nil
}
