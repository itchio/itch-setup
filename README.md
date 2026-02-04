# itch-setup

This is the install and self-update helper for [the itch.io app][app]

https://itchio.itch.io/itch-setup

It applies a few tricks it learned from Squirrel.Mac and Squirrel.Windows, and
uses some of the same technology behind [butler][], itch.io's command line
uploader and patcher. 

[app]: https://itch.io/app
[butler]: https://itch.io/docs/butler

### What does it look like?

Something like this:

![](https://user-images.githubusercontent.com/7998310/39475360-428bd3ce-4d58-11e8-9e9d-720b8e7d31aa.png)

This is a screen you should only see the first time you launch the itch.io app,
or if your itch install gets corrupted on disk somehow.

Any upgrades happen seamlessly in the background.

## How It Works

### Overview

itch-setup is responsible for installing, updating, and launching the itch.io desktop app. It handles:

- First-time installation with a graphical progress indicator
- Background updates while the app is running
- Relaunching after updates are applied
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

