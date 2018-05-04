package setup

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/itchio/wharf/eos/option"

	"github.com/itchio/savior/seeksource"

	"github.com/itchio/httpkit/timeout"
	"github.com/itchio/wharf/pwr"
	"github.com/pkg/errors"

	humanize "github.com/dustin/go-humanize"
	"github.com/itchio/itch-setup/localize"
	"github.com/itchio/wharf/eos"
	"github.com/itchio/wharf/state"

	_ "github.com/itchio/wharf/decompressors/cbrotli"
	_ "github.com/itchio/wharf/decompressors/zstd"
)

type ErrorHandler func(err error)
type ProgressLabelHandler func(label string)
type ProgressHandler func(progress float64)
type FinishHandler func(source InstallSource)
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
	sourceChan chan InstallSource

	channelName string
	consumer    *state.Consumer
}

type InstallSource struct {
	Version string
}

const brothBaseURL = "https://broth.itch.ovh"

func NewInstaller(settings InstallerSettings) *Installer {
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	channelName := fmt.Sprintf("%s-%s", goos, goarch)

	i := &Installer{
		settings:    settings,
		sourceChan:  make(chan InstallSource),
		channelName: channelName,
		consumer: &state.Consumer{
			OnMessage: func(lvl string, msg string) {
				log.Printf("[%s] %s", lvl, msg)
			},
		},
	}

	go func() {
		err := i.warmUp()
		if err != nil {
			log.Printf("Install error: %s", err.Error())
			settings.OnError(err)
		}
	}()
	return i
}

func (i *Installer) brothPackageURL() string {
	return fmt.Sprintf("%s/%s/%s", brothBaseURL, i.settings.AppName, i.channelName)
}

func (i *Installer) warmUp() error {
	version, err := i.getVersion()
	if err != nil {
		return errors.WithMessage(err, "while getting latest version")
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

	client := timeout.NewDefaultClient()

	latestURL := fmt.Sprintf("%s/LATEST", i.brothPackageURL())
	latestRes, err := client.Get(latestURL)
	if err != nil {
		return "", errors.WithMessage(err, "looking for latest version")
	}

	if latestRes.StatusCode != 200 {
		return "", errors.Errorf("got HTTP %d for %s", latestRes.StatusCode, latestURL)
	}

	versionBytes, err := ioutil.ReadAll(latestRes.Body)
	if err != nil {
		return "", errors.WithMessage(err, "reading latest version")
	}

	version := strings.TrimSpace(string(versionBytes))
	return version, nil
}

func (i *Installer) Install(appDir string) {
	go func() {
		installSource := <-i.sourceChan
		err := i.doInstall(appDir, installSource)
		if err != nil {
			i.settings.OnError(err)
		} else {
			i.settings.OnFinish(installSource)
		}
	}()
}

func (i *Installer) doInstall(appDir string, installSource InstallSource) error {
	ctx := context.Background()
	localizer := i.settings.Localizer

	i.settings.OnProgressLabel(localizer.T("setup.status.preparing"))

	version := installSource.Version

	signatureURL := fmt.Sprintf("%s/%s/signature", i.brothPackageURL(), version)
	archiveURL := fmt.Sprintf("%s/%s/archive", i.brothPackageURL(), version)

	sigFile, err := eos.Open(signatureURL, option.WithConsumer(i.consumer))
	if err != nil {
		return errors.WithMessage(err, "while opening remote signature file")
	}
	defer sigFile.Close()

	log.Printf("Downloading signature...")
	sigBytes, err := ioutil.ReadAll(sigFile)
	if err != nil {
		return errors.WithMessage(err, "reading remote signature file")
	}

	sigSource := seeksource.FromBytes(sigBytes)
	_, err = sigSource.Resume(nil)
	if err != nil {
		return errors.WithMessage(err, "while opening signature file")
	}

	sigInfo, err := pwr.ReadSignature(ctx, sigSource)
	if err != nil {
		return errors.WithMessage(err, "while parsing signature file")
	}

	container := sigInfo.Container
	log.Printf("To install: %s", container.Stats())

	startTime := time.Now()

	consumer := newConsumer()
	consumer.OnProgress = func(progress float64) {
		percent := int(progress * 100.0)
		doneSize := int64(float64(container.Size) * progress)
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
	}

	log.Printf("Installing to %s", appDir)

	healPath := fmt.Sprintf("archive,%s", archiveURL)

	vc := pwr.ValidatorContext{
		Consumer: consumer,
		HealPath: healPath,
	}

	log.Printf("Healing %s...", appDir)
	err = vc.Validate(ctx, appDir, sigInfo)
	if err != nil {
		return errors.WithMessage(err, "while installing")
	}

	sigPath := localSignaturePath(appDir)
	log.Printf("Writing signature to %s", sigPath)

	err = ioutil.WriteFile(sigPath, sigBytes, os.FileMode(0644))
	if err != nil {
		return errors.WithMessage(err, "while writing local signature file")
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
