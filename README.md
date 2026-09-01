# itch-setup

This is the installer, launcher and self-update helper for [the itch.io app][app]

https://itchio.itch.io/itch-setup

It applies a few tricks it learned from Squirrel.Mac and Squirrel.Windows, and
uses some of the same technology behind [butler][], itch.io's command line
uploader and patcher.

[app]: https://itch.io/app
[butler]: https://itch.io/docs/butler

## itch-setup as the app's launcher

In a typical installation, itch-setup is a small, stable binary that sits in
front of the app. Every way of starting the app on the system goes through it:

- Desktop and Start Menu shortcuts
- The `.desktop` file on Linux
- The `itch:` / `itchio:` URL protocol handlers
- Steam shortcuts and other launchers (see [Launching Games in Headless
  Mode](#launching-games-in-headless-mode))

All of these invoke itch-setup with `--prefer-launch`, forwarding any
arguments (such as a URL) after `--`:

```
itch-setup --prefer-launch --appname itch -- "%1"
```

The app itself is installed side by side in versioned directories
(`app-<version>/`), and `state.json` records which one is current and whether
a newer one has been downloaded and is ready to go. When itch-setup is
launched as a proxy it:

1. Promotes a downloaded "ready" version to current, if there is one (on
   Windows this is skipped while the current version is still running)
2. Validates that the current version is intact
3. Starts it, passing through any arguments

If no valid version is found (first run, or a corrupted install), it falls
back to running the full installer, shown below, and launches the app
afterwards.

Because shortcuts always point at itch-setup rather than at a specific
`app-<version>/` executable, updating the app never breaks them, and the app
is free to replace its own files: while running, it periodically invokes
`itch-setup --upgrade` to download the next version in the background, then
`itch-setup --relaunch` to swap over to it.

### What does the installer look like?

Something like this:

![](https://user-images.githubusercontent.com/7998310/39475360-428bd3ce-4d58-11e8-9e9d-720b8e7d31aa.png)

This is a screen you should only see the first time you launch the itch.io app,
or if your itch install gets corrupted on disk somehow.

Upgrades happen in the background.

## How It Works

### Overview

itch-setup is responsible for installing, updating, and launching the itch.io desktop app. It handles:

- First-time installation with a graphical progress indicator
- Launching the current version on every app start (see above)
- Background updates while the app is running
- Relaunching after updates are applied
- Launching installed games headlessly, without the app running
- Uninstallation

### Command Line Flags

| Flag | Description |
|------|-------------|
| `--prefer-launch` | Try to launch an existing installation first; only run setup if no valid version is found |
| `--upgrade` | Check for and apply updates (used by the running app for background updates) |
| `--relaunch` | Wait for a process to exit, then relaunch the app (used after applying updates) |
| `--relaunch-pid <pid>` | PID to wait for before relaunching (required with `--relaunch`) |
| `--uninstall` | Remove the installation |
| `--appname <name>` | Specify which app to manage: `itch` or `kitch` |
| `--silent` | Run installation without showing the GUI |
| `--no-fallback` | Disable automatic arm64 to amd64 architecture fallback |
| `--info` | Display installation information and exit |
| `--run-game <id>` | Launch an installed itch.io game by its game ID, headlessly through butler when possible. If the app isn't installed, falls back to the installer GUI and forwards the launch to the app afterwards |
| `--profile-id <id>` | itch.io user ID to attribute play sessions to (used with `--run-game`) |
| `--sync-launcher` | Refresh the launcher copy of itch-setup and the desktop integration (shortcuts, protocol handlers) from the binary being run. Used by the app after a self-update; no-op on macOS |
| `--elevate` | Re-run itch-setup with administrator rights before doing anything else (Windows only). Used with `--upgrade` when the install folder isn't writable |
| `--log-file <path>` | Also write JSON-lines status messages to this file, so a caller can read the outcome of `--upgrade` / `--relaunch` |
| `-- <args...>` | Everything after `--` is passed through to the itch app's command line (Linux and Windows only) |

`--run-game` and `--sync-launcher` imply `--silent`.

#### Passing Arguments Through to the App

Arguments after `--` are appended to the app's own command line. The
shortcuts, `.desktop` file and `itch:` / `itchio:` URL protocol registrations
that itch-setup creates all go through this path, so the OS always launches
the stable `itch-setup` binary rather than a versioned `app-X.Y.Z` executable:

```
itch-setup --prefer-launch --appname itch -- "%1"      # Windows protocol handler
itch-setup --prefer-launch --appname itch -- "$@"      # Linux launch script
```

You can use the same shape by hand to open a URL in the currently installed
version of the app. For example, this is the URL `--run-game` hands to the app
to install a game if needed and then launch it:

```
itch-setup --prefer-launch -- "itch://install?game_id=12345&launch"
```

For kitch, use `--appname kitch` and a `kitch://` URL, since kitch only
handles its own scheme.

`--prefer-launch` makes itch-setup launch the current install without going
through setup, and the URL is handed to the app exactly as it would be by a
click on an `itchio:` link. If no valid install is found, setup runs first and
the URL is forwarded once the app is installed.

#### Environment Variables

| Variable | Description |
|----------|-------------|
| `ITCHSETUP_VERSION` | Install this version instead of the latest one, see [Installing a Specific Version](#installing-a-specific-version) |
| `ITCH_BROTH_URL` | Base URL of the Broth package server (default `https://broth.itch.zone`). Used by the integration tests to point at a mock server |
| `ITCH_SETUP_LOCALE` | Force the UI locale (e.g. `fr`) instead of detecting it from the system |

### Installation Flow

1. **Fetch latest version** - Query the Broth package server for the latest version number
2. **Download signature** - Fetch the archive signature file for verification
3. **Stream and extract** - Download the archive while extracting files, using wharf's "healing" mechanism
4. **Stage in temp folder** - New installs go to a staging directory first
5. **Swap atomically** - Move the staged version to the final location, renaming any existing version to `.old`
6. **Create shortcuts** - Set up desktop shortcuts, start menu entries, or `.desktop` files (platform-specific)
7. **Launch the app** - Start the newly installed itch app

For updates, the same process is used but the new version is queued as "ready" and applied on the next relaunch.

### File Locations

| Platform | Base Directory | App Location |
|----------|---------------|--------------|
| Windows | `%LOCALAPPDATA%\itch\` | `%LOCALAPPDATA%\itch\app-<version>\` |
| Linux | `~/.itch/` | `~/.itch/app-<version>/` |
| macOS | `~/Library/Application Support/itch-setup/` | `~/Applications/itch.app` |

Each installation directory contains:
- `state.json` - Tracks current and ready versions
- `app-<version>/` - The installed app files (or staging directory during install)
- `staging/` - Temporary directory used during installation

### Version Management

itch-setup uses a "multiverse" system to manage versions, tracked in `state.json`:

```json
{
  "current": "25.6.2",
  "ready": ""
}
```

- **current** - The version that's installed and actively used
- **ready** - A version that's been downloaded but not yet activated (pending relaunch)

When an update is downloaded, it's stored as "ready". On the next relaunch (via `--relaunch`), the ready version becomes current.

### Launching Games in Headless Mode

Installed games can be launched without the app running:

```
itch-setup --run-game <gameId> [--profile-id <profileId>]
```

This was introduced primarily for the app's Steam shortcuts feature, but it
works with any launcher or shortcut system. `--run-game` implies `--silent`,
so no window is shown; itch-setup stays alive until the game exits and
forwards termination signals to it.

This is built on butler's `launch` subcommand, which runs the app's launch
machinery in-process against the existing `butler.db`, with no daemon
required. itch-setup handles the app-specific parts:

1. Locates the app's user data directory and the broth-managed butler the
   app itself uses (`<userData>/broth/butler/`), so the database schema
   always matches.
2. Reads `preferences.json` and translates the relevant global settings into
   butler `--default-*` flags. Per-game launch options come from the
   database and take precedence.
3. Runs `butler --json --dbpath <userData>/db/butler.db launch --game <gameId> ...`
   and mirrors its log output.

Games launched this way behave as if started from the app: sandboxing,
prerequisites, play time tracking, API key injection and per-game launch
options are all preserved.

`--profile-id` attributes the play session to that itch.io user (Steam
shortcuts bake in the profile that created them). If that profile has since
been logged out of the app, the launch is retried without it, so only the
session attribution is lost.

Not every launch can run headlessly. HTML5 games
need a browser window, soundtracks and books need the OS shell, and an
unaccepted license or a failed prerequisite install needs a client to prompt
the user. In those cases (or if the installed butler is too old to support
`launch`) itch-setup hands off to the full app instead, starting it or
bringing it to the foreground on the game's launch dialog via an
`itch://install?game_id=<gameId>&launch` URL. If the app itself isn't
installed, the regular installer GUI runs and the URL is forwarded once the
install completes.

### Installing a Specific Version

By default itch-setup installs whatever Broth reports as the latest version.
To install a specific version instead, set the `ITCHSETUP_VERSION` environment
variable before running the installer:

```
# Linux / macOS
ITCHSETUP_VERSION=25.6.2 ./itch-setup

# Windows (cmd)
set ITCHSETUP_VERSION=25.6.2
itch-setup.exe

# Windows (PowerShell)
$env:ITCHSETUP_VERSION = "25.6.2"
.\itch-setup.exe
```

When set, itch-setup skips the `LATEST` lookup and fetches that version's
archive directly, so it must exist on Broth for your channel. The list of
available versions for a channel is at:

```
https://broth.itch.zone/itch/<channel>/versions
```

where `<channel>` is e.g. `linux-amd64`, `windows-amd64` or `darwin-arm64`.

Caveats:

- The override only applies to the install flow (running `itch-setup` with no
  flags, or with `--silent`). It is not honored by `--upgrade`, which
  always compares against `LATEST`.
- Don't combine it with `--prefer-launch`: if a valid version is already
  installed, that flag launches it without running setup at all.
- If the requested version is already the current one, itch-setup just heals
  it in place. Otherwise the requested version is staged, then made current,
  even if it's older than what's installed.
- The pin is not persistent. The next time the running app performs a
  background update check via `--upgrade`, it will download and queue the
  latest version as usual.

### Uninstall

Run `itch-setup --uninstall` to remove the installation. The uninstaller will:

1. **Kill running processes** - Gracefully close any running instances of the app
2. **Remove installation files** - Delete all versioned app directories (`app-<version>/`), icons, state files, and shortcuts
3. **Clean app-managed data** - Remove logs, crash reports, and prerequisites from the user data directory

**What gets preserved:**

User data is intentionally kept to allow easy reinstallation:
- User profiles and accounts (`users/`)
- Preferences (`preferences.json`, `config.json`)
- Game library database (`db/`)

**Platform-specific details:**

| Platform | Removes | User Data Location |
|----------|---------|-------------------|
| Windows | `%LOCALAPPDATA%\itch\`, Start Menu & Desktop shortcuts, registry uninstaller entry | `%APPDATA%\itch\` |
| macOS | `~/Applications/itch.app`, `~/Library/Application Support/itch-setup/` | `~/Library/Application Support/itch/` |
| Linux | `~/.itch/`, `~/.local/share/applications/io.itch.itch.desktop` | `~/.config/itch/` |

On Windows, the `itch-setup.exe` binary cannot delete itself while running, so it moves itself to a temporary trash directory (`%TEMP%\.itch-setup-trash\`).

### Broth

[Broth](https://broth.itch.zone) is itch.io's package distribution service. itch-setup fetches packages from Broth at URLs like:

```
https://broth.itch.zone/itch/linux-amd64/LATEST
https://broth.itch.zone/itch/linux-amd64/<version>/archive/default
```

### Architecture Fallback

On macOS and Windows, if you're running on an arm64 system (Apple Silicon or ARM Windows) and no native arm64 build is available on Broth, itch-setup will automatically fall back to the amd64 version. This works because:

- macOS with Apple Silicon can run x86_64 apps via Rosetta 2
- Windows on ARM can run x64 apps via emulation

This allows itch-setup to install the app even when a native ARM build hasn't been released yet. Use the `--no-fallback` flag to disable this behavior and require a native arm64 build.

### License

itch-setup is MIT-licensed, see LICENSE for details

