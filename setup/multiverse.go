package setup

import (
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pkg/errors"
)

type Multiverse interface {
	// true if the basedir had a marker
	// the rest of the fields won't make sense
	IsValid() bool

	GetValidAppDir() (string, bool)
	GetAppDir(version string) string
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

	if len(m.appDirs) == 0 {
		return "", false
	}

	appDir := m.appDirs[0]
	absDir := filepath.Join(m.params.BaseDir, appDir)

	return absDir, true
}

func (m *multiverse) GetAppDir(version string) string {
	name := fmt.Sprintf("app-%s", version)
	return filepath.Join(m.params.BaseDir, name)
}

func NewMultiverse(params *MultiverseParams) (Multiverse, error) {
	if params.AppName == "" {
		return nil, errors.Errorf("MultiverParams.AppName cannot be empty")
	}

	if params.BaseDir == "" {
		return nil, errors.Errorf("MultiverParams.BaseDir cannot be empty")
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
