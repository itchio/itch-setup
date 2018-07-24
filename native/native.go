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

	// Shows an error dialog (with stack trace and repo link)
	// and exits afterwards.
	ErrorDialog(err error)
}
