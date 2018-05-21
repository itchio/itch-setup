package setup

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/itchio/go-itchio"

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
	client      *http.Client
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
		client: timeout.NewDefaultClient(),
	}

	return i
}

func (i *Installer) brothPackageURL() string {
	return fmt.Sprintf("%s/%s/%s", brothBaseURL, i.settings.AppName, i.channelName)
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

	latestVersion, err := i.brothGetString("/LATEST")
	if err != nil {
		return "", err
	}

	return latestVersion, nil
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

type BrothUpgradePath struct {
	Patches []BrothPatch `json:"patches"`
}

type BrothPatch struct {
	Version string            `json:"version"`
	Files   []*BrothPatchFile `json:"files"`
}

type BrothPatchFile struct {
	SubType itchio.BuildFileSubType `json:"subType"`
	Size    int64                   `json:"size"`
}

func (i *Installer) brothGetBytes(format string, args ...interface{}) ([]byte, error) {
	subpath := fmt.Sprintf(format, args...)
	url := fmt.Sprintf("%s/%s", i.brothPackageURL(), strings.Trim(subpath, "/"))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, errors.Errorf("Could not build GET request to %s", url)
	}

	res, err := i.client.Do(req)
	if err != nil {
		return nil, errors.WithMessage(err, fmt.Sprintf("While performing GET request to %s", url))
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return nil, errors.Errorf("Got HTTP %d for %s", res.StatusCode, url)
	}

	bs, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, errors.WithMessage(err, fmt.Sprintf("While reading GET request to %s", url))
	}

	return bs, nil
}

func (i *Installer) brothGetString(format string, args ...interface{}) (string, error) {
	bs, err := i.brothGetBytes(format, args...)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(bs)), nil
}

func (i *Installer) brothGetResponse(r interface{}, format string, args ...interface{}) error {
	bs, err := i.brothGetBytes(format, args...)
	if err != nil {
		return err
	}

	err = json.Unmarshal(bs, r)
	if err != nil {
		return errors.WithMessage(err, "unmarshalling broth response")
	}

	return nil
}

func (i *Installer) Upgrade(mv Multiverse) error {
	appDir, ok := mv.GetValidAppDir()
	if !ok {
		return errors.Errorf("No valid app dir found in %s", mv.GetBaseDir())
	}

	currentVersion := strings.TrimPrefix(filepath.Base(appDir), "app-")
	log.Printf("Installed: %s", currentVersion)

	latestVersion, err := i.brothGetString("/LATEST")
	if err != nil {
		return err
	}
	log.Printf("Latest: %s", latestVersion)

	upgradePath := &BrothUpgradePath{}
	err = i.brothGetResponse(upgradePath, "/%s/upgrade-paths/%s",
		currentVersion,
		latestVersion,
	)
	if err != nil {
		return err
	}

	return nil
}
