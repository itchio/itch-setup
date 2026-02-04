package setup

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/itchio/ox"
	"github.com/itchio/savior/filesource"

	"github.com/itchio/headway/state"
	"github.com/itchio/headway/united"

	"github.com/itchio/httpkit/eos/option"
	"github.com/itchio/httpkit/timeout"

	_ "github.com/itchio/wharf/decompressors/brotli"
	"github.com/itchio/wharf/pwr"

	"github.com/itchio/itch-setup/localize"
)

type ErrorHandler func(err error)
type ProgressLabelHandler func(label string)
type ProgressHandler func(progress float64)
type FinishHandler func(source InstallSource)
type SourceHandler func(source InstallSource)

type InstallerSettings struct {
	AppName         string
	Localizer       *localize.Localizer
	NoFallback      bool
	OnError         ErrorHandler
	OnProgressLabel ProgressLabelHandler
	OnProgress      ProgressHandler
	OnFinish        FinishHandler
	OnSource        SourceHandler
}

type Installer struct {
	settings   InstallerSettings
	sourceChan chan InstallSource

	channelName       string
	consumer          *state.Consumer
	client            *http.Client
	downloadSessionID string
}

type InstallSource struct {
	Version string
}

const brothBaseURL = "https://broth.itch.zone"

func NewInstaller(settings InstallerSettings) *Installer {
	runtime := ox.CurrentRuntime()
	channelName := fmt.Sprintf("%s-%s", runtime.OS(), runtime.Arch())

	i := &Installer{
		settings:    settings,
		sourceChan:  make(chan InstallSource),
		channelName: channelName,
		consumer: &state.Consumer{
			OnMessage: func(lvl string, msg string) {
				log.Printf("[%s] %s", lvl, msg)
			},
		},
		client:            timeout.NewDefaultClient(),
		downloadSessionID: uuid.New().String(),
	}

	return i
}

func (i *Installer) brothPackageURL() string {
	return fmt.Sprintf("%s/%s/%s", brothBaseURL, i.settings.AppName, i.channelName)
}

func (i *Installer) buildBrothURL(values url.Values, format string, args ...interface{}) string {
	if values == nil {
		values = make(url.Values)
	}
	values.Set("downloadSessionId", i.downloadSessionID)
	formattedPath := fmt.Sprintf(format, args...)
	return fmt.Sprintf("%s/%s?%s", i.brothPackageURL(), formattedPath, values.Encode())
}

func (i *Installer) WarmUp() {
	go func() {
		err := i.warmUp()
		if err != nil {
			log.Printf("Install error: %s", err.Error())
			i.settings.OnError(err)
		}
	}()
}

func (i *Installer) resolveChannel() error {
	exists, err := i.checkChannelExists()
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	// Channel doesn't exist - check if we can fall back to amd64
	rt := ox.CurrentRuntime()
	if !i.settings.NoFallback && (rt.OS() == "darwin" || rt.OS() == "windows") && rt.Arch() == "arm64" {
		fallbackChannel := fmt.Sprintf("%s-amd64", rt.OS())
		log.Printf("Channel %s not found, falling back to %s", i.channelName, fallbackChannel)
		i.channelName = fallbackChannel
		return nil
	}

	return fmt.Errorf("channel %s not found", i.channelName)
}

func (i *Installer) warmUp() error {
	if err := i.resolveChannel(); err != nil {
		return fmt.Errorf("while resolving channel: %w", err)
	}

	version, err := i.getVersion()
	if err != nil {
		return fmt.Errorf("while getting latest version: %w", err)
	}

	log.Printf("Will install version %s\n", version)

	source := InstallSource{
		Version: version,
	}
	if i.settings.OnSource != nil {
		i.settings.OnSource(source)
	}
	i.sourceChan <- source
	return nil
}

func (i *Installer) getVersion() (string, error) {
	envVersion := os.Getenv("ITCHSETUP_VERSION")
	if envVersion != "" {
		log.Printf("Version overriden by environment: %s", envVersion)
		return envVersion, nil
	}

	latestVersion, err := i.brothGetString("/LATEST")
	if err != nil {
		return "", err
	}

	return latestVersion, nil
}

func (i *Installer) Install(mv Multiverse) {
	go func() {
		installSource := <-i.sourceChan
		err := i.doInstall(mv, installSource)
		if err != nil {
			i.settings.OnError(err)
		} else {
			i.settings.OnFinish(installSource)
		}
	}()
}

func (i *Installer) doInstall(mv Multiverse, installSource InstallSource) error {
	ctx := context.Background()
	localizer := i.settings.Localizer

	i.settings.OnProgressLabel(localizer.T("setup.status.preparing"))

	version := installSource.Version

	signatureURL := i.buildBrothURL(nil, "%s/signature/default", version)
	archiveURL := i.buildBrothURL(nil, "%s/archive/default", version)

	sigSource, err := filesource.Open(signatureURL, option.WithConsumer(i.consumer))
	if err != nil {
		return fmt.Errorf("while opening remote signature file: %w", err)
	}
	defer sigSource.Close()

	log.Printf("Reading signature...")
	sigInfo, err := pwr.ReadSignature(ctx, sigSource)
	if err != nil {
		return fmt.Errorf("while parsing signature file: %w", err)
	}

	container := sigInfo.Container
	log.Printf("Installing %s", container)

	startTime := time.Now()

	consumer := newConsumer()
	consumer.OnProgress = func(progressVal float64) {
		percent := int(progressVal * 100.0)
		doneSize := int64(float64(container.Size) * progressVal)
		secsSinceStart := time.Since(startTime).Seconds()
		donePerSec := int64(float64(doneSize) / float64(secsSinceStart))

		percentStr := fmt.Sprintf("%d%%", percent)
		speedStr := fmt.Sprintf("%s/s", united.FormatBytes(donePerSec))

		progressLabel := fmt.Sprintf("%s - %s",
			localizer.T("setup.status.progress", map[string]string{"percent": percentStr}),
			localizer.T("setup.status.installing", map[string]string{"speed": speedStr}),
		)
		i.settings.OnProgressLabel(progressLabel)
		i.settings.OnProgress(progressVal)
	}

	useStaging := false

	var appDir string
	currentBuildFolder := mv.GetCurrentVersion()
	if currentBuildFolder != nil && currentBuildFolder.Version == version {
		log.Printf("Looks like (%s) is already installed to (%s)", version, currentBuildFolder.Path)
		log.Printf("Let's just heal that")
		appDir = currentBuildFolder.Path
	} else {
		log.Printf("(%s) is not installed yet, let's go through staging", version)
		if currentBuildFolder != nil {
			log.Printf("(Note: current version is (%s) at this time)", currentBuildFolder.Version)
		}
		useStaging = true

		stagingFolder, err := mv.MakeStagingFolder()
		if err != nil {
			return err
		}
		defer mv.CleanStagingFolder()
		appDir = filepath.Join(stagingFolder, fmt.Sprintf("app-%s", version))
	}

	log.Printf("Installing to (%s)", appDir)

	healPath := fmt.Sprintf("archive,%s", archiveURL)

	vc := pwr.ValidatorContext{
		Consumer: consumer,
		HealPath: healPath,
	}

	log.Printf("Healing (%s)...", appDir)
	err = vc.Validate(ctx, appDir, sigInfo)
	if err != nil {
		return fmt.Errorf("while installing: %w", err)
	}

	duration := time.Since(startTime)

	wc := vc.WoundsConsumer
	if wc != nil {
		if ah, ok := wc.(*pwr.ArchiveHealer); ok {
			log.Printf("%s was healed @ %s (%s total)",
				united.FormatBytes(ah.TotalHealed()),
				united.FormatBPS(ah.TotalHealed(), duration),
				united.FormatDuration(duration),
			)
		}
	}

	if useStaging {
		log.Printf("Used staging, queuing as ready then making current...")
		err = mv.QueueReady(&BuildFolder{
			Path:    appDir,
			Version: version,
		})
		if err != nil {
			return err
		}

		err = mv.MakeReadyCurrent()
		if err != nil {
			return err
		}
	} else {
		log.Printf("Healed in-place")
		err = mv.ValidateCurrent()
		if err != nil {
			return err
		}
	}

	i.settings.OnProgressLabel(localizer.T("setup.status.done"))
	return nil
}

func localSignaturePath(appDir string) string {
	return filepath.Join(appDir, "signature.pws")
}

func newConsumer() *state.Consumer {
	return &state.Consumer{
		OnMessage: func(lvl string, msg string) {
			log.Printf("[%s] %s", lvl, msg)
		},
	}
}
