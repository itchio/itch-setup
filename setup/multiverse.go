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

type ValidateHandler func(dir string) error

type multiverseState struct {
	// Current is the version that's installed and used.
	// It might be currently running at that point.
	Current string `json:"current"`

	// Ready is a version we've installed but aren't using yet.
	// It'll be used on next launch or when `--relaunch` is
	// called.
	Ready string `json:"ready"`
}

type BuildFolder struct {
	Version string
	Path    string
}

type Multiverse interface {
	// Called on launch, or when upgrading
	GetCurrentVersion() *BuildFolder

	// Called when we start patching
	MakeStagingFolder() (string, error)
	// defer'd at the end of patching
	CleanStagingFolder() error

	// Record a freshly-patched build as ready
	QueueReady(build *BuildFolder) error

	// Returns true if we have a ready build pending
	HasReadyPending() bool

	// Returns true if the ready pending version is 'version'
	ReadyPendingIs(version string) bool

	// Make the ready build current.
	MakeReadyCurrent() error

	// Validates the current build (can be used after heal)
	ValidateCurrent() error

	// Returns a human-friendly representation of the state of this multiverse
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

	// This is called with a folder before making it the current version
	OnValidate ValidateHandler
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

func (mv *multiverse) GetCurrentVersion() *BuildFolder {
	currentVersion := mv.state.Current
	if currentVersion == "" {
		return nil
	}

	build := &BuildFolder{
		Version: currentVersion,
		Path:    mv.makePathForCurrent(currentVersion),
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

func (mv *multiverse) QueueReady(build *BuildFolder) error {
	s := mv.state
	if s.Ready != "" {
		log.Printf("Replacing ready (%s) with (%s)", s.Ready, build.Version)
	} else {
		log.Printf("Queuing ready (%s)", build.Version)
	}

	if !filepath.IsAbs(build.Path) {
		return errors.Errorf("Internal error: Ready BuildFolder must have absolute path, but got (%s)", build.Path)
	}

	readyPath := filepath.Join(mv.params.BaseDir, mv.versionToBasename(build.Version))
	log.Printf("Storing in (%s)", readyPath)

	err := os.RemoveAll(readyPath)
	if err != nil {
		return errors.WithMessage(err, "making sure ready version's folder does not exist")
	}

	err = os.Rename(build.Path, readyPath)
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

func (mv *multiverse) HasReadyPending() bool {
	return mv.state.Ready != ""
}

func (mv *multiverse) ReadyPendingIs(version string) bool {
	if !mv.HasReadyPending() {
		return false
	}
	return mv.state.Ready == version
}

func (mv *multiverse) MakeReadyCurrent() error {
	s := mv.state
	if s.Ready == "" {
		return errors.Errorf("No ready to make current")
	}

	log.Printf("Attempting to make (%s) the current version over (%s)", s.Ready, s.Current)
	readyPath := filepath.Join(mv.params.BaseDir, mv.versionToBasename(s.Ready))

	err := mv.validateDir(readyPath)
	if err != nil {
		return err
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

	newCurrentPath := mv.makePathForCurrent(s.Ready)

	if readyPath == newCurrentPath {
		log.Printf("(%s) already at right location", readyPath)
	} else {
		log.Printf("Renaming (%s) to (%s)", readyPath, newCurrentPath)
		err := os.MkdirAll(filepath.Dir(newCurrentPath), 0755)
		if err != nil {
			return err
		}

		err = os.Rename(readyPath, newCurrentPath)
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
	err = mv.saveState()
	if err != nil {
		return err
	}

	return nil
}

func (mv *multiverse) ValidateCurrent() error {
	return mv.validateDir(mv.makePathForCurrent(mv.state.Current))
}

func (mv *multiverse) validateDir(dir string) error {
	if mv.params.OnValidate == nil {
		log.Printf("No validate handler, assuming good!")
		return nil
	}

	log.Printf("Validating (%s) ...", dir)
	err := mv.params.OnValidate(dir)
	if err != nil {
		return errors.WithMessage(err, "while validating new version")
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

	mv.state = state

	return nil
}

func (mv *multiverse) saveState() error {
	bs, err := json.Marshal(mv.state)
	if err != nil {
		return errors.WithMessage(err, "marshalling multiverse state file")
	}

	err = os.MkdirAll(filepath.Dir(mv.statePath()), 0755)
	if err != nil {
		return errors.WithMessage(err, "creating folder for multiverse state file")
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
	return fmt.Sprintf("(%s)(current = %q, ready = %q)", mv.params.BaseDir, mv.state.Current, mv.state.Ready)
}
