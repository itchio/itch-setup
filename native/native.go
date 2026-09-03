package native

// The Core type is where platform-specific actions are
// implemented, often using cross-platform facilities (but not always).
type Core interface {
	// Perform install from scratch or heals existing installation
	Install() error

	// Remove existing installation (all versions)
	Uninstall() error

	// Looks for new versions, applies patches, signals update
	// progress and whether a relaunch is needed.
	Upgrade() error

	// Waits for PID to exit, then opens latest version of
	// the app. On macOS, moves latest to /Applications before
	// launching
	Relaunch() error

	// Upgrade followed by Relaunch in one process, so that an
	// elevated run (see RelaunchElevated) needs a single UAC prompt
	UpgradeAndRelaunch() error

	// Re-executes itch-setup with administrator rights and the same
	// arguments. Returns true if that happened and this process should
	// exit. Only meaningful on Windows.
	RelaunchElevated() (bool, error)

	// Shows an error dialog (with stack trace and repo link)
	// and exits afterwards.
	ErrorDialog(err error)

	// Shows info in CLI and quit
	Info()

	// Launches an installed itch.io game headlessly through the app's
	// butler, waiting for it to exit; hands the launch to the app when
	// butler can't serve it.
	RunGame(gameID int64) error

	// Refreshes the stable launcher copy of itch-setup (and desktop
	// integration) from this binary. Run by the app after updating its
	// broth-managed itch-setup: app-update events are otherwise the
	// only thing that propagates a new itch-setup to the paths shims
	// and shortcuts point at.
	SyncLauncher() error

	// Asks a running app to quit, through the app's own --shutdown
	// flag. A no-op when no app is installed.
	Shutdown() error
}
