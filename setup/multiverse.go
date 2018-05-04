package setup

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/itchio/savior/seeksource"

	"github.com/itchio/wharf/pwr"

	"github.com/pkg/errors"
)

type Multiverse interface {
	// true if the basedir had a marker
	// the rest of the fields won't make sense
	IsValid() bool

	GetValidAppDir() (string, bool)
	MakeAppDir(version string) (string, error)
	ListAppDirs() []string
	GetBaseDir() string
}

type multiverse struct {
	params      *MultiverseParams
	foundMarker bool
	appDirs     []string
}

type MultiverseParams struct {
	// `itch`, `kitch`
	AppName string

	// on linux, this would be `~/.itch`
	// on windows, this would be `%LOCALAPPDATA%/itch`
	BaseDir string
}

func (m *multiverse) IsValid() bool {
	return m.foundMarker
}

func (m *multiverse) GetValidAppDir() (string, bool) {
	if !m.IsValid() {
		return "", false
	}

	for _, appDir := range m.appDirs {
		absDir := filepath.Join(m.params.BaseDir, appDir)

		err := m.validateAppDir(absDir)
		if err != nil {
			log.Printf("Ignoring appDir %s: %+v", absDir, err)
			continue
		}

		return absDir, true
	}
	return "", false
}

func (m *multiverse) validateAppDir(appDir string) error {
	sigPath := localSignaturePath(appDir)
	sigBytes, err := ioutil.ReadFile(sigPath)
	if err != nil {
		return err
	}

	sigSource := seeksource.FromBytes(sigBytes)

	_, err = sigSource.Resume(nil)
	if err != nil {
		return err
	}

	ctx := context.Background()

	sigInfo, err := pwr.ReadSignature(ctx, sigSource)
	if err != nil {
		return err
	}

	log.Printf("Found valid wharf signature at %s", sigPath)

	vc := &pwr.ValidatorContext{
		Consumer: newConsumer(),
		FailFast: true,
	}

	err = vc.Validate(ctx, appDir, sigInfo)
	if err != nil {
		return err
	}

	log.Printf("App dir matches signature: %s", appDir)
	return nil
}

func (m *multiverse) ListAppDirs() []string {
	var appDirs []string

	for _, appDir := range m.appDirs {
		appDirs = append(appDirs, filepath.Join(m.params.BaseDir, appDir))
	}
	return appDirs
}

func (m *multiverse) MakeAppDir(version string) (string, error) {
	// first make sure we have a marker
	markerPath := filepath.Join(m.params.BaseDir, m.markerName())
	markerWriter, err := os.Create(markerPath)
	if err != nil {
		return "", err
	}
	defer markerWriter.Close()

	name := fmt.Sprintf("app-%s", version)
	appDir := filepath.Join(m.params.BaseDir, name)
	return appDir, nil
}

func (m *multiverse) GetBaseDir() string {
	return m.params.BaseDir
}

func NewMultiverse(params *MultiverseParams) (Multiverse, error) {
	if params.AppName == "" {
		return nil, errors.Errorf("MultiverseParams.AppName cannot be empty")
	}

	if params.BaseDir == "" {
		return nil, errors.Errorf("MultiverseParams.BaseDir cannot be empty")
	}

	mv := &multiverse{
		params: params,
	}
	log.Printf("Initializing '%s' multiverse @ %s", params.AppName, params.BaseDir)

	entries, err := ioutil.ReadDir(params.BaseDir)
	if err != nil {
		log.Printf("Empty (%s), that's ok", params.BaseDir)
		return mv, nil
	}

	log.Printf("Looking through %d entries...", len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			if entry.Name() == mv.markerName() {
				log.Printf("Found marker...")
				mv.foundMarker = true
			}
			continue
		}

		if !strings.HasPrefix(entry.Name(), "app-") {
			continue
		}

		log.Printf("Found app dir %s", entry.Name())
		mv.appDirs = append(mv.appDirs, entry.Name())
	}

	if len(mv.appDirs) == 0 {
		log.Printf("No app dirs in sight, it's install time!")
		return mv, nil
	}

	log.Printf("Found %d app dirs, sorting them from most recent to least recent...", len(mv.appDirs))
	sort.Sort(sort.Reverse(sort.StringSlice(mv.appDirs)))

	return mv, nil
}

func (mv *multiverse) markerName() string {
	return fmt.Sprintf(".%s-marker", mv.params.AppName)
}
