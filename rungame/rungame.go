// Package rungame launches an installed itch.io game headlessly through
// the app's broth-managed butler, handing the launch to the app itself
// when butler reports it cannot serve it. itch-setup owns all the app
// conventions here (userData layout, preferences.json, broth); butler is
// invoked with everything spelled out in flags.
package rungame

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode"
)

// Exit code butler uses to signal the launch needs the app, accompanied
// by a "launch/needs-app" JSON line.
const needsAppExitCode = 3

// Exit code butler uses to signal that the requested profile is gone
// from the database (logged out since the shortcut baked it).
const profileGoneExitCode = 4

type Params struct {
	AppName     string
	UserDataDir string

	// Profile to attribute play sessions to, usually baked into the
	// shortcut at creation time. Zero lets butler resolve one.
	ProfileID int64

	// LaunchApp starts the installed app with the given arguments and
	// extra environment, and returns without waiting for it.
	LaunchApp func(args []string, extraEnv []string) error
}

func Run(params Params, gameID int64) error {
	butlerExe, err := findButler(params.UserDataDir)
	if err != nil {
		log.Printf("Could not find a usable butler: %+v", err)
		return launchApp(params, gameID, 0)
	}

	if !supportsLaunch(butlerExe) {
		log.Printf("(%s) has no launch command, handing off to the app", butlerExe)
		return launchApp(params, gameID, 0)
	}

	baseArgs := []string{
		"--json",
		"--dbpath", filepath.Join(params.UserDataDir, "db", "butler.db"),
		"launch",
		"--game", strconv.FormatInt(gameID, 10),
		"--prereqs-dir", filepath.Join(params.UserDataDir, "prereqs"),
	}
	baseArgs = append(baseArgs, defaultFlags(params.UserDataDir)...)

	args := baseArgs
	if params.ProfileID != 0 {
		args = append(args, "--profile-id", strconv.FormatInt(params.ProfileID, 10))
	}

	res, err := runButler(butlerExe, args)
	if err != nil {
		return err
	}

	if res.exitCode == profileGoneExitCode && res.profileGone && !res.stopped && params.ProfileID != 0 {
		// the baked profile logged out since the shortcut was created;
		// keep the launch headless, only session attribution is lost
		log.Printf("Profile (%d) no longer exists, retrying without it", params.ProfileID)
		params.ProfileID = 0
		res, err = runButler(butlerExe, baseArgs)
		if err != nil {
			return err
		}
	}

	if res.stopped {
		log.Printf("butler stopped by our forwarded signal, exiting")
		return nil
	}

	switch res.exitCode {
	case 0:
		log.Printf("Game exited")
		return nil
	case needsAppExitCode:
		if res.needsApp == nil {
			// the code alone could mean anything from another butler
			// version; only the payload makes it a handoff
			return fmt.Errorf("butler launch failed with exit code %d (no needs-app payload)", res.exitCode)
		}
		log.Printf("butler needs the app: %s", res.needsApp.reason)
		return launchApp(params, gameID, res.needsApp.uploadID)
	default:
		return fmt.Errorf("butler launch failed with exit code %d", res.exitCode)
	}
}

type butlerResult struct {
	exitCode int

	// non-nil only when a launch/needs-app payload was seen
	needsApp *needsAppInfo

	// a launch/profile-not-found payload was seen
	profileGone bool

	// butler exited nonzero after we forwarded it a stop signal; a
	// cancellation, not a failure
	stopped bool
}

func runButler(butlerExe string, args []string) (butlerResult, error) {
	log.Printf("Running (%s) %s", butlerExe, strings.Join(args, " "))

	// butler inherits the environment untouched: launcher preloads (like
	// Steam's overlay) must reach the game
	cmd := exec.Command(butlerExe, args...)
	cmd.SysProcAttr = sysProcAttr()
	cmd.Stderr = newLogWriter("butler[err]")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return butlerResult{}, err
	}

	if err := cmd.Start(); err != nil {
		return butlerResult{}, fmt.Errorf("starting butler: %w", err)
	}
	tieProcessLifetime(cmd.Process.Pid)

	// a launcher stopping us (or Ctrl+C) should stop butler, which stops
	// the game and records the play session on its way out
	var forwarded atomic.Bool
	sigs := make(chan os.Signal, 2)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer func() {
		// Stop guarantees no further sends, making close safe
		signal.Stop(sigs)
		close(sigs)
	}()
	go func() {
		for sig := range sigs {
			log.Printf("Got %s, forwarding to butler", sig)
			// a signal that wasn't delivered (Windows, or butler already
			// gone) can't explain butler's exit
			if cmd.Process.Signal(sig) == nil {
				forwarded.Store(true)
			}
		}
	}()

	var res butlerResult
	res.needsApp, res.profileGone = scanButlerOutput(stdout)

	err = cmd.Wait()
	if err != nil {
		exitError, ok := err.(*exec.ExitError)
		if !ok {
			return butlerResult{}, fmt.Errorf("waiting on butler: %w", err)
		}
		res.exitCode = exitError.ExitCode()
		res.stopped = forwarded.Load()
	}
	return res, nil
}

// ErrAppNotInstalled means there is no valid app version to hand the
// launch to; wrapped by LaunchApp implementations so callers can fall
// back to the installer.
var ErrAppNotInstalled = errors.New("app is not installed")

// AppNotInstalledError carries the handoff URL alongside
// ErrAppNotInstalled so the installer fallback can preserve it (butler
// may have picked a specific upload).
type AppNotInstalledError struct {
	URL string
	err error
}

func (e *AppNotInstalledError) Error() string { return e.err.Error() }
func (e *AppNotInstalledError) Unwrap() error { return e.err }

// InstallAndLaunchURL is the URL that makes the app install the game if
// needed and launch it. The scheme follows the app: kitch only handles
// kitch:// urls.
func InstallAndLaunchURL(appName string, gameID int64, uploadID int64) string {
	if uploadID != 0 {
		return fmt.Sprintf("%s://install?game_id=%d&upload_id=%d&launch", appName, gameID, uploadID)
	}
	return fmt.Sprintf("%s://install?game_id=%d&launch", appName, gameID)
}

func launchApp(params Params, gameID int64, uploadID int64) error {
	url := InstallAndLaunchURL(params.AppName, gameID, uploadID)
	log.Printf("Handing launch to the app: %s", url)
	var extraEnv []string
	if params.ProfileID != 0 {
		// a freshly booted app logs into this saved profile instead of
		// showing the gate; env can't reach an already-running app, which
		// keeps whatever profile state it has
		extraEnv = append(extraEnv, fmt.Sprintf("ITCH_PROFILE_ID=%d", params.ProfileID))
	}
	err := params.LaunchApp([]string{url}, extraEnv)
	if errors.Is(err, ErrAppNotInstalled) {
		return &AppNotInstalledError{URL: url, err: err}
	}
	return err
}

type needsAppInfo struct {
	reason   string
	uploadID int64
}

// scanButlerOutput mirrors butler's JSON log lines into our log and
// captures the launch/needs-app and launch/profile-not-found payloads
// if emitted. Reads until EOF, which arrives when butler exits.
func scanButlerOutput(r io.Reader) (needsApp *needsAppInfo, profileGone bool) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var obj map[string]interface{}
		if json.Unmarshal(scanner.Bytes(), &obj) != nil {
			log.Printf("butler: %s", scanner.Text())
			continue
		}
		switch obj["type"] {
		case "log":
			log.Printf("butler: %v", obj["message"])
		case "launch/needs-app":
			info := &needsAppInfo{}
			info.reason, _ = obj["reason"].(string)
			if v, ok := obj["uploadId"].(float64); ok {
				info.uploadID = int64(v)
			}
			needsApp = info
		case "launch/profile-not-found":
			profileGone = true
		}
	}
	if err := scanner.Err(); err != nil {
		// we're the only pipe reader: keep draining or butler blocks on
		// a full pipe and Wait never returns
		log.Printf("reading butler output: %v", err)
		_, _ = io.Copy(io.Discard, r)
	}
	return needsApp, profileGone
}

// findButler resolves the app's current broth-managed butler, the same
// binary the app's butlerd runs, so its schema always matches the DB.
func findButler(userDataDir string) (string, error) {
	brothDir := filepath.Join(userDataDir, "broth", "butler")

	versionBytes, err := os.ReadFile(filepath.Join(brothDir, ".chosen-version"))
	if err != nil {
		return "", err
	}
	version := strings.TrimSpace(string(versionBytes))
	if version == "" {
		return "", fmt.Errorf("empty .chosen-version in (%s)", brothDir)
	}

	versionDir := filepath.Join(brothDir, "versions", version)
	if _, err := os.Stat(filepath.Join(versionDir, ".installed")); err != nil {
		return "", fmt.Errorf("butler (%s) is not marked installed", version)
	}

	exeName := "butler"
	if runtime.GOOS == "windows" {
		exeName = "butler.exe"
	}
	exe := filepath.Join(versionDir, exeName)
	if _, err := os.Stat(exe); err != nil {
		return "", err
	}
	return exe, nil
}

// supportsLaunch probes for the launch command instead of comparing
// versions: `launch --help` exits 0 exactly when the command exists.
// Bounded so a butler that can't even run --help (held by AV, truncated)
// degrades to the app handoff instead of hanging the shortcut forever.
func supportsLaunch(butlerExe string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, butlerExe, "launch", "--help")
	cmd.SysProcAttr = sysProcAttr()
	return cmd.Run() == nil
}

// appPreferences is the slice of the app's preferences.json the runner
// cares about; missing or unparseable values degrade to no defaults.
type appPreferences struct {
	IsolateApps           bool   `json:"isolateApps"`
	LinuxSandboxType      string `json:"linuxSandboxType"`
	LinuxSandboxNoNetwork bool   `json:"linuxSandboxNoNetwork"`
	LinuxSandboxAllowEnv  string `json:"linuxSandboxAllowEnv"`
}

// defaultFlags translates the app's global preferences into butler's
// --default-* flags. Per-game overrides stay butler's business: it reads
// cave settings itself and applies these only underneath them.
func defaultFlags(userDataDir string) []string {
	data, err := os.ReadFile(filepath.Join(userDataDir, "preferences.json"))
	if err != nil {
		log.Printf("No app preferences: %+v", err)
		return nil
	}
	var prefs appPreferences
	if err := json.Unmarshal(data, &prefs); err != nil {
		log.Printf("Could not parse app preferences: %+v", err)
		return nil
	}

	var flags []string
	if prefs.IsolateApps {
		flags = append(flags, "--default-sandbox")
	}
	// the app only sends sandbox options on Linux
	if runtime.GOOS == "linux" {
		if prefs.LinuxSandboxType != "" && prefs.LinuxSandboxType != "auto" {
			flags = append(flags, "--default-sandbox-type", prefs.LinuxSandboxType)
		}
		if prefs.LinuxSandboxNoNetwork {
			flags = append(flags, "--default-sandbox-no-network")
		}
		for _, name := range parseAllowEnv(prefs.LinuxSandboxAllowEnv) {
			flags = append(flags, "--default-sandbox-allow-env", name)
		}
	}
	return flags
}

// parseAllowEnv mirrors the app's parseSandboxAllowEnv: comma or
// whitespace separated names, duplicates dropped.
func parseAllowEnv(rawText string) []string {
	var result []string
	seen := make(map[string]bool)
	fields := strings.FieldsFunc(rawText, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
	for _, name := range fields {
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		result = append(result, name)
	}
	return result
}

// EnvWithoutOverlayPreload strips Steam's overlay renderer from the
// preload list, keeping everything else (MangoHud, compat shims). Games
// want the overlay; the app's Chromium startup deadlocks under it.
func EnvWithoutOverlayPreload() []string {
	var env []string
	for _, kv := range os.Environ() {
		for _, name := range []string{"LD_PRELOAD=", "DYLD_INSERT_LIBRARIES="} {
			if strings.HasPrefix(kv, name) {
				if kept := withoutOverlayEntries(strings.TrimPrefix(kv, name)); kept != "" {
					kv = name + kept
				} else {
					kv = ""
				}
				break
			}
		}
		if kv != "" {
			env = append(env, kv)
		}
	}
	return env
}

// preload lists separate entries with colons or spaces; rejoining with
// colons is always valid
func withoutOverlayEntries(list string) string {
	var kept []string
	for _, entry := range strings.FieldsFunc(list, func(r rune) bool {
		return r == ':' || r == ' '
	}) {
		if strings.Contains(filepath.Base(entry), "gameoverlayrenderer") {
			continue
		}
		kept = append(kept, entry)
	}
	return strings.Join(kept, ":")
}

type logWriter struct {
	prefix string
}

func newLogWriter(prefix string) logWriter {
	return logWriter{prefix: prefix}
}

func (w logWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		log.Printf("%s: %s", w.prefix, line)
	}
	return len(p), nil
}
