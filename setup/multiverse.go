package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/dchest/safefile"
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

	// Returns the version of the ready build pending, or "" if none
	GetReadyVersion() string

	// Re-reads durable state and returns the ready version pending, or
	// "" if none. For reporting after long operations, where in-memory
	// state may be stale.
	ReloadReadyVersion() (string, error)

	// Discard the ready build, removing its folder unless it doubles as
	// the current version's folder
	ClearReady() error

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
		return nil, fmt.Errorf("MultiverseParams.AppName cannot be empty")
	}

	if params.BaseDir == "" {
		return nil, fmt.Errorf("MultiverseParams.BaseDir cannot be empty")
	}

	mv := &multiverse{
		params: params,
		state:  &multiverseState{},
	}
	log.Printf("Initializing (%s) multiverse @ (%s)", params.AppName, params.BaseDir)

	err := mv.readState()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
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
	return mv.withStateLock(func() error {
		return mv.queueReady(build)
	})
}

func (mv *multiverse) queueReady(build *BuildFolder) error {
	// another process may have changed state.json since we read it, e.g.
	// a relaunch promoting the previous ready build while we downloaded.
	// If we can't read the durable state, don't rename or delete
	// anything based on the cached view.
	if err := mv.refreshState(); err != nil {
		return fmt.Errorf("re-reading multiverse state: %w", err)
	}

	s := mv.state
	oldReady := s.Ready
	if oldReady != "" {
		log.Printf("Replacing ready (%s) with (%s)", oldReady, build.Version)
	} else {
		log.Printf("Queuing ready (%s)", build.Version)
	}

	if !filepath.IsAbs(build.Path) {
		return fmt.Errorf("Internal error: Ready BuildFolder must have absolute path, but got (%s)", build.Path)
	}

	readyPath := filepath.Join(mv.params.BaseDir, mv.versionToBasename(build.Version))
	log.Printf("Storing in (%s)", readyPath)

	err := os.RemoveAll(readyPath)
	if err != nil {
		return fmt.Errorf("making sure ready version's folder does not exist: %w", err)
	}

	err = os.Rename(build.Path, readyPath)
	if err != nil {
		return fmt.Errorf("moving ready version to its proper place: %w", err)
	}

	s.Ready = build.Version
	err = mv.saveState()
	if err != nil {
		// keep the in-memory state matching the durable one, so callers
		// reporting the ready version don't report an uncommitted value
		s.Ready = oldReady
		return fmt.Errorf("updating multiverse state with new ready: %w", err)
	}

	if oldReady != "" && oldReady != build.Version && oldReady != s.Current {
		oldReadyPath := filepath.Join(mv.params.BaseDir, mv.versionToBasename(oldReady))
		log.Printf("Removing superseded ready (%s)", oldReadyPath)
		err := os.RemoveAll(oldReadyPath)
		if err != nil {
			log.Printf("Could not remove superseded ready: %v", err)
		}
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

func (mv *multiverse) GetReadyVersion() string {
	return mv.state.Ready
}

func (mv *multiverse) ReloadReadyVersion() (string, error) {
	var version string
	err := mv.withStateLock(func() error {
		err := mv.refreshState()
		if err != nil {
			return err
		}
		version = mv.state.Ready
		return nil
	})
	return version, err
}

func (mv *multiverse) ClearReady() error {
	return mv.withStateLock(mv.clearReady)
}

func (mv *multiverse) clearReady() error {
	if err := mv.refreshState(); err != nil {
		return fmt.Errorf("re-reading multiverse state: %w", err)
	}

	s := mv.state
	if s.Ready == "" {
		return nil
	}

	oldReady := s.Ready
	log.Printf("Discarding ready (%s)", oldReady)

	// clear the state pointer first: a state referencing a missing folder
	// is worse than an orphaned folder
	s.Ready = ""
	err := mv.saveState()
	if err != nil {
		s.Ready = oldReady
		return fmt.Errorf("clearing ready from multiverse state: %w", err)
	}

	if oldReady != s.Current {
		oldReadyPath := filepath.Join(mv.params.BaseDir, mv.versionToBasename(oldReady))
		log.Printf("Removing discarded ready (%s)", oldReadyPath)
		err := os.RemoveAll(oldReadyPath)
		if err != nil {
			log.Printf("Could not remove discarded ready: %v", err)
		}
	}

	return nil
}

// withStateLock runs fn holding the cross-process state lock, so state
// transitions (queueing, promoting, clearing ready) can't interleave
// between processes.
func (mv *multiverse) withStateLock(fn func() error) error {
	lock, err := acquireStateLock(mv.params.BaseDir)
	if err != nil {
		return err
	}
	if lock == nil {
		log.Printf("Proceeding without state lock")
		return fn()
	}
	defer lock.release()
	return fn()
}

// refreshState re-reads state.json, which another process may have
// modified. A missing or corrupt file keeps the in-memory state: there
// is no valid durable state to defer to, and rewriting the file is how a
// corrupt one heals. An I/O error is returned, as valid state may exist
// that we just can't see, and transitions must not run on a stale view.
func (mv *multiverse) refreshState() error {
	err := mv.readState()
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) {
		log.Printf("State file is corrupt, proceeding with in-memory state: %v", err)
		return nil
	}
	return err
}

func (mv *multiverse) MakeReadyCurrent() error {
	return mv.withStateLock(mv.makeReadyCurrent)
}

func (mv *multiverse) makeReadyCurrent() error {
	// a long-running upgrade may have superseded the ready build we read
	// at startup; promote what state.json says now, or nothing at all if
	// the durable state can't be read
	if err := mv.refreshState(); err != nil {
		return fmt.Errorf("re-reading multiverse state: %w", err)
	}

	s := mv.state
	if s.Ready == "" {
		return fmt.Errorf("No ready to make current")
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
		_, statErr := os.Stat(currentBuild.Path)
		if statErr == nil {
			// current build path exists
			currentBuildSave = currentBuild.Path + ".old"
			log.Printf("Renaming (%s) to (%s)", currentBuild.Path, currentBuildSave)

			retries := 5
			var renameErr error
			for i := 0; i < retries; i++ {
				renameErr = os.Rename(currentBuild.Path, currentBuildSave)
				if renameErr == nil {
					break
				}
				log.Printf("Rename failed (attempt %d/%d): %+v", i+1, retries, renameErr)
				if i < retries-1 && isRetryableRenameError(renameErr) {
					log.Printf("Retrying in 2 seconds...")
					time.Sleep(2 * time.Second)
				} else {
					break
				}
			}
			if renameErr != nil {
				return renameErr
			}
		} else {
			currentBuild = nil
			log.Printf("Was going to back up current's folder, but got: %v", statErr)
			log.Printf("This means state.json didn't match what was actually on disk")
			log.Printf("Let's just go with the ready version and cross fingers")
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
		return fmt.Errorf("while validating new version: %w", err)
	}
	return nil
}

func isRetryableRenameError(err error) bool {
	if err == nil || runtime.GOOS != "windows" {
		return false
	}

	const windowsErrorSharingViolation syscall.Errno = 32
	const windowsErrorLockViolation syscall.Errno = 33

	return errors.Is(err, windowsErrorSharingViolation) || errors.Is(err, windowsErrorLockViolation)
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
	bs, err := os.ReadFile(mv.statePath())
	if err != nil {
		return fmt.Errorf("reading multiverse state file: %w", err)
	}

	state := &multiverseState{}
	err = json.Unmarshal(bs, state)
	if err != nil {
		return fmt.Errorf("unmarshalling multiverse state file: %w", err)
	}

	mv.state = state

	return nil
}

func (mv *multiverse) saveState() error {
	bs, err := json.Marshal(mv.state)
	if err != nil {
		return fmt.Errorf("marshalling multiverse state file: %w", err)
	}

	err = os.MkdirAll(filepath.Dir(mv.statePath()), 0755)
	if err != nil {
		return fmt.Errorf("creating folder for multiverse state file: %w", err)
	}

	f, err := safefile.Create(mv.statePath(), 0644)
	if err != nil {
		return fmt.Errorf("creating multiverse state file: %w", err)
	}
	defer f.Close()

	_, err = f.Write(bs)
	if err != nil {
		return fmt.Errorf("writing multiverse state file: %w", err)
	}

	err = f.Commit()
	if err != nil {
		return fmt.Errorf("committing multiverse state file: %w", err)
	}

	return nil
}

func (mv *multiverse) String() string {
	return fmt.Sprintf("(%s)(current = %q, ready = %q)", mv.params.BaseDir, mv.state.Current, mv.state.Ready)
}
