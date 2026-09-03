package cl

import "github.com/itchio/itch-setup/localize"

// globals, get your globals here!

type CLI struct {
	AppName       string
	VersionString string

	Localizer *localize.Localizer

	PreferLaunch bool
	Upgrade      bool
	Uninstall    bool
	Info         bool
	Relaunch     bool
	RelaunchPID  int
	RunGameID    int64
	ProfileID    int64
	SyncLauncher bool
	Shutdown     bool

	// Re-exec with administrator rights before running the verb
	// (Windows only). Used by the app when the install folder isn't
	// writable by the current user.
	Elevate bool
	// Install root resolved by the caller, for the elevated re-exec:
	// under another administrator's account, its own lookup would
	// land in that account's profile.
	InstallDir string
	// Mirror JSON-lines messages to this file. An elevated process can't
	// inherit the app's stdout, so this is how it reports back.
	LogFile string

	Silent     bool
	NoFallback bool
	Args       []string
}
