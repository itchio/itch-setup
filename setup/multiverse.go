package setup

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"

	"github.com/dchest/safefile"
	"github.com/pkg/errors"
)

type multiverseState struct {
	// Current is the version that's installed and used.
	// It might be currently running at that point.
	Current string

	// Ready is a version we've installed but aren't using yet.
	// It'll be used on next launch or when `--relaunch` is
	// called.
	Ready string
}

type InstalledBuild struct {
	Version string
	Path    string
}

type Multiverse interface {
	// Called on launch, or when upgrading
	GetCurrentVersion() *InstalledBuild

	// Called when we start patching
	MakeStagingFolder() (string, error)
	// defer'd at the end of patching
	CleanStagingFolder() error

	// Record a freshly-patched build as ready
	QueueReady(build *InstalledBuild) error

	// Make the ready build current.
	MakeReadyCurrent() error

	String() string
}

type multiverse struct {
	params *MultiverseParams
	state  *multiverseState
}

type MultiverseParams struct {
	// `itch`, `kitch`
	AppName string

	// on Linux, `~/.itch`
	// on Windows, `%LOCALAPPDATA%/itch`
	// on macOS, `~/Library/Application Support/itch-setup`
	BaseDir string

	// If non-empty, this is where we'll store current
	// on macOS, this is `~/Applications`
	ApplicationsDir string
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
		state:  &multiverseState{},
	}
	log.Printf("Initializing (%s) multiverse @ (%s)", params.AppName, params.BaseDir)

	err := mv.readState()
	if err != nil {
		if os.IsNotExist(errors.Cause(err)) {
			log.Printf("No multiverse information yet")
		} else {
			log.Printf("Ignoring: %v", err)
		}
	} else {
		log.Printf("%s", mv)
	}

	return mv, nil
}

func (mv *multiverse) GetCurrentVersion() *InstalledBuild {
	current := mv.state.Current
	if current == "" {
		return nil
	}

	build := &InstalledBuild{
		Version: current,
		Path:    mv.versionToBasename(current),
	}
	return build
}

func (mv *multiverse) MakeStagingFolder() (string, error) {
	path := mv.stagingFolderPath()
	err := os.RemoveAll(path)
	if err != nil {
		return "", err
	}

	err = os.MkdirAll(path, 0755)
	if err != nil {
		return "", err
	}

	return path, nil
}

func (mv *multiverse) CleanStagingFolder() error {
	path := mv.stagingFolderPath()
	return os.RemoveAll(path)
}

func (mv *multiverse) QueueReady(build *InstalledBuild) error {
	s := mv.state
	if s.Ready != "" {
		log.Printf("Replacing ready (%s) with (%s)", s.Ready, build.Version)
	} else {
		log.Printf("Queuing ready (%s)", build.Version)
	}

	readyPath := filepath.Join(mv.params.BaseDir, mv.versionToBasename(build.Version))
	log.Printf("Storing in (%s)", readyPath)

	err := os.Rename(build.Path, readyPath)
	if err != nil {
		return errors.WithMessage(err, "moving ready version to its proper place")
	}

	s.Ready = build.Version
	err = mv.saveState()
	if err != nil {
		return errors.WithMessage(err, "updating multiverse state with new ready")
	}

	return nil
}

func (mv *multiverse) MakeReadyCurrent() error {
	s := mv.state
	if s.Ready == "" {
		return errors.Errorf("No ready to make current")
	}

	currentBuild := mv.GetCurrentVersion()
	var currentBuildSave string
	if currentBuild != nil {
		currentBuildSave = currentBuild.Path + ".old"
		log.Printf("Renaming (%s) to (%s)", currentBuild.Path, currentBuildSave)
		err := os.Rename(currentBuild.Path, currentBuildSave)
		if err != nil {
			return err
		}
	}

	readyPath := filepath.Join(mv.params.BaseDir, mv.versionToBasename(s.Ready))
	newCurrentPath := mv.makePathForCurrent(s.Ready)

	if readyPath == newCurrentPath {
		log.Printf("(%s) already at right location", readyPath, newCurrentPath)
	} else {
		log.Printf("Renaming (%s) to (%s)", readyPath, newCurrentPath)
		err := os.Rename(readyPath, newCurrentPath)
		if err != nil {
			if currentBuild != nil {
				os.Rename(currentBuildSave, currentBuild.Path)
			}
			return err
		}
	}

	if currentBuild != nil {
		log.Printf("Cleaning up (%s)", currentBuildSave)
		os.RemoveAll(currentBuildSave)
	}

	s.Current = s.Ready
	s.Ready = ""
	err := mv.saveState()
	if err != nil {
		return err
	}

	return nil
}

func (mv *multiverse) makePathForCurrent(version string) string {
	p := mv.params
	if p.ApplicationsDir != "" {
		bundleName := fmt.Sprintf("%s.app", mv.params.AppName)
		return filepath.Join(p.ApplicationsDir, bundleName)
	}

	return filepath.Join(p.BaseDir, mv.versionToBasename(version))
}

func (mv *multiverse) versionToBasename(version string) string {
	return fmt.Sprintf("app-%s", version)
}

func (mv *multiverse) stagingFolderPath() string {
	return filepath.Join(mv.params.BaseDir, "staging")
}

func (mv *multiverse) statePath() string {
	return filepath.Join(mv.params.BaseDir, "state.json")
}

func (mv *multiverse) readState() error {
	bs, err := ioutil.ReadFile(mv.statePath())
	if err != nil {
		return errors.WithMessage(err, "reading multiverse state file")
	}

	state := &multiverseState{}
	err = json.Unmarshal(bs, state)
	if err != nil {
		return errors.WithMessage(err, "unmarshalling multiverse state file")
	}

	return nil
}

func (mv *multiverse) saveState() error {
	bs, err := json.Marshal(mv.state)
	if err != nil {
		return errors.WithMessage(err, "marshalling multiverse state file")
	}

	f, err := safefile.Create(mv.statePath(), 0644)
	if err != nil {
		return errors.WithMessage(err, "creating multiverse state file")
	}
	defer f.Close()

	_, err = f.Write(bs)
	if err != nil {
		return errors.WithMessage(err, "writing multiverse state file")
	}

	err = f.Commit()
	if err != nil {
		return errors.WithMessage(err, "committing multiverse state file")
	}

	return nil
}

func (mv *multiverse) String() string {
	return fmt.Sprintf("(%s)(current = %q, ready = %q)", mv.params.BaseDir, mv.state.Ready, mv.state.Current)
}
