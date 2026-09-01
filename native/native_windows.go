package native

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/scjalliance/comshim"
	"github.com/skratchdot/open-golang/open"

	"github.com/itchio/itch-setup/cl"
	"github.com/itchio/itch-setup/native/nwin"
	"github.com/itchio/itch-setup/rungame"
	"github.com/itchio/itch-setup/setup"
	"github.com/lxn/walk"
	ui "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

type nativeCore struct {
	cli cl.CLI

	mainWindow *walk.MainWindow
	folders    nwin.Folders
	baseDir    string
	user       *nwin.UserProfile
}

// NewCore returns a windows-specific Core implementation
func NewCore(cli cl.CLI) (Core, error) {
	nc := &nativeCore{cli: cli, user: nwin.CurrentUserProfile()}

	// The elevated copy may run under a different administrator's
	// account (a standard user typing someone else's credentials at the
	// UAC prompt). Folders, shortcuts and HKCU keys still belong to the
	// user who asked.
	// No falling back to our own profile: that would put everything in
	// the wrong account, and the app couldn't be started as the desktop
	// user afterwards anyway.
	if cli.Elevate && nwin.IsElevated() {
		user, err := nwin.DesktopUserProfile()
		if err != nil {
			return nil, fmt.Errorf("resolving the desktop user to update for: %w", err)
		}
		nc.user = user
	}
	log.Printf("Per-user integration goes to: %s", nc.user)

	folders, err := nc.user.Folders()
	if err != nil {
		return nil, fmt.Errorf("During setup initialization: %w", err)
	}

	nc.folders = folders

	defaultBaseDir := filepath.Join(folders.LocalAppData, cli.AppName)
	baseDir := defaultBaseDir

	registryBaseDir, err := nwin.GetRegistryInstallDir(cli, nc.user)
	if err != nil {
		log.Printf("Could not get registry base dir: %+v", err)
	} else {
		log.Printf("Default base dir:  (%s)", defaultBaseDir)
		log.Printf("Registry base dir: (%s)", registryBaseDir)
		if defaultBaseDir == registryBaseDir {
			log.Printf("Same as default, moving on")
		} else {
			log.Printf("Strays from defaults, taking it into account")
			baseDir = registryBaseDir
		}
	}
	if cli.InstallDir != "" {
		// resolved by the caller as the right user; our own lookup
		// above may have run under someone else's profile
		if !filepath.IsAbs(cli.InstallDir) {
			return nil, fmt.Errorf("--install-dir must be an absolute path, got (%s)", cli.InstallDir)
		}
		log.Printf("Install dir from command line: (%s)", cli.InstallDir)
		baseDir = filepath.Clean(cli.InstallDir)
	}
	log.Printf("Initial base dir: (%s)", baseDir)
	nc.baseDir = baseDir

	// Our working directory may be a versioned app folder, inherited from
	// a stale shortcut via the app that spawned us. Windows refuses to
	// rename a directory any process is sitting in, and that rename is
	// how updates get promoted, so move somewhere stable. If this fails,
	// keep going: promotion has its own failure handling, and our
	// directory may well have been safe already.
	if _, err := os.Stat(baseDir); err == nil {
		previous, _ := os.Getwd()
		err := os.Chdir(baseDir)
		if err != nil {
			log.Printf("Could not change working directory from (%s) to (%s): %v", previous, baseDir, err)
		} else {
			log.Printf("Changed working directory from (%s) to (%s)", previous, baseDir)
		}
	}

	return nc, nil
}

func (nc *nativeCore) Install() error {
	comshim.Add(1)
	defer comshim.Done()

	cli := nc.cli

	if cli.PreferLaunch {
		log.Printf("Launch preferred, looking for a valid app folder")
		mv, err := nc.newMultiverse()
		if err != nil {
			log.Printf("Could not make multiverse: %v", err)
			log.Printf("Won't be able to launch.")
		} else {
			err := nc.tryLaunchCurrent(mv, nil)
			if err != nil {
				log.Printf("While launching current: %+v", err)
				log.Printf("Continuing with setup...")
			}
		}
	}

	log.Printf("Showing install GUI")
	return nc.showInstallGUI()
}

func (nc *nativeCore) Upgrade() error {
	comshim.Add(1)
	defer comshim.Done()

	cli := nc.cli

	mv, err := nc.newMultiverse()
	if err != nil {
		return err
	}

	installer := setup.NewInstaller(setup.InstallerSettings{
		Localizer:  cli.Localizer,
		AppName:    cli.AppName,
		NoFallback: cli.NoFallback,
	})

	// Program Files and friends: the app runs us as the plain user, and
	// nothing below would succeed. Report what's available and let the
	// app come back with --elevate.
	if !nc.isWritableInstallDir(nc.baseDir) {
		log.Printf("Install folder (%s) isn't writable, only checking for updates", nc.baseDir)
		return installer.CheckUpgrade(mv)
	}

	res, err := installer.Upgrade(mv)
	if err != nil {
		return err
	}

	if res.DidUpgrade {
		err = nc.doPostInstall(mv, PostInstallParams{
			ForUpgrade: true,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (nc *nativeCore) UpgradeAndRelaunch() error {
	err := nc.Upgrade()
	if err != nil {
		return err
	}
	return nc.Relaunch()
}

func (nc *nativeCore) RelaunchElevated() (bool, error) {
	if nwin.IsElevated() {
		log.Printf("Already elevated, not re-executing")
		return false, nil
	}

	// --elevate stays on the command line: the elevated copy lands in
	// the "already elevated" case above, and it tells startApp that the
	// app must be handed back to the desktop user. The install dir goes
	// along too, since the elevated copy may not share our profile.
	args := os.Args[1:]
	if nc.cli.InstallDir == "" {
		args = append([]string{"--install-dir", nc.baseDir}, args...)
	}
	err := nwin.RelaunchElevated(args)
	if err != nil {
		return false, err
	}
	return true, nil
}

type PostInstallParams struct {
	ForUpgrade bool
}

func (nc *nativeCore) doPostInstall(mv setup.Multiverse, params PostInstallParams) error {
	installDir := nc.baseDir
	cli := nc.cli

	currentBuild := mv.GetCurrentVersion()
	if currentBuild == nil {
		return fmt.Errorf("internal error (in post-install with a nil currentBuild)")
	}

	setupLocalPath, err := CopySelf(filepath.Join(installDir, "itch-setup.exe"))
	if err != nil {
		nc.ErrorDialog(err)
		return err
	}

	// this creates $installDir/app.ico
	nc.syncUninstallRegistryEntry(currentBuild.Version)

	// this needs to be done before the shortcut is created
	err = nc.writeVisualElementsManifest()
	if err != nil {
		return err
	}

	err = nwin.RegisterURLProtocols(cli, nc.user, setupLocalPath)
	if err != nil {
		log.Printf("While registering URL protocols: %+v", err)
		log.Printf("Ignoring protocol registration error and continuing...")
	}

	shortcutArguments := fmt.Sprintf("--prefer-launch --appname %s", cli.AppName)

	for _, spec := range nc.shortcutSpecs() {
		log.Printf("Creating shortcut (%s)...", spec.Path)
		onlyIfExists := spec.OnlyIfExists || params.ForUpgrade

		err = nwin.CreateShortcut(nwin.ShortcutSettings{
			ShortcutFilePath: spec.Path,
			OnlyIfExists:     onlyIfExists,
			TargetPath:       setupLocalPath,
			Arguments:        shortcutArguments,
			Description:      "The best way to play your itch.io games",
			IconLocation:     filepath.Join(installDir, "app.ico"),
			WorkingDirectory: filepath.Join(installDir),
			AppUserModelId:   "io.itch.itch",
		})
		if err != nil {
			log.Printf("While creating shortcut: %+v", err)
			log.Printf("Ignoring shortcut creation error and continuing...")
		}
	}

	nc.migrateStaleShortcuts(setupLocalPath, shortcutArguments)

	return nil
}

// migrateStaleShortcuts repairs shortcuts, outside the set we manage, that
// point directly at a versioned app-X.Y.Z executable in our install root.
// Windows shell registration/pinning can materialize those, and they keep
// launching an obsolete build after a self-update. Only shortcuts whose
// resolved target is <installDir>\app-<version>\<appname>.exe are touched.
func (nc *nativeCore) migrateStaleShortcuts(setupLocalPath string, shortcutArguments string) {
	candidates := []string{
		// created by Windows shell app registration, not by us
		filepath.Join(nc.folders.Programs, nc.shortcutName()),
	}

	// Taskbar pins made while no shortcut of ours carried an
	// AppUserModelId: the shell could not match the window to a shortcut,
	// so it wrote its own, pointing at the running executable.
	implicitPattern := filepath.Join(nc.folders.RoamingAppData, "Microsoft", "Internet Explorer", "Quick Launch", "ImplicitAppShortcuts", "*", "*.lnk")
	implicit, err := filepath.Glob(implicitPattern)
	if err != nil {
		log.Printf("Could not list implicit app shortcuts (%s): %+v", implicitPattern, err)
	} else {
		candidates = append(candidates, implicit...)
	}

	for _, lnkPath := range candidates {
		if _, err := os.Stat(lnkPath); err != nil {
			continue
		}

		target, err := nwin.GetShortcutTarget(lnkPath)
		if err != nil {
			log.Printf("Could not inspect shortcut (%s), leaving it alone: %+v", lnkPath, err)
			continue
		}

		if !setup.IsStaleAppShortcutTarget(target, nc.baseDir, nc.exeName()) {
			log.Printf("Shortcut (%s) -> (%s) is not ours to fix, leaving it alone", lnkPath, target)
			continue
		}

		log.Printf("Migrating stale shortcut (%s) -> (%s) to the launcher", lnkPath, target)
		err = nwin.CreateShortcut(nwin.ShortcutSettings{
			ShortcutFilePath: lnkPath,
			TargetPath:       setupLocalPath,
			Arguments:        shortcutArguments,
			Description:      "The best way to play your itch.io games",
			IconLocation:     filepath.Join(nc.baseDir, "app.ico"),
			WorkingDirectory: filepath.Join(nc.baseDir),
			AppUserModelId:   "io.itch.itch",
		})
		if err != nil {
			log.Printf("While migrating shortcut: %+v", err)
			log.Printf("Ignoring shortcut migration error and continuing...")
		}
	}
}

func (nc *nativeCore) syncUninstallRegistryEntry(version string) {
	err := nwin.CreateUninstallRegistryEntry(nc.cli, nc.user, nc.baseDir, version)
	if err != nil {
		log.Printf("While creating registry entry: %+v", err)
		log.Printf("Ignoring uninstall registry entry creation error and continuing...")
	}
}

func (nc *nativeCore) Relaunch() error {
	cli := nc.cli

	mv, err := nc.newMultiverse()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	err = setup.WaitForProcessToExit(ctx, cli.RelaunchPID)
	if err != nil {
		log.Printf("Giving up on PID %d: %+v", cli.RelaunchPID, err)
		log.Printf("Attempting to launch anyway...")
	}

	// The main process exiting isn't enough: renderer/GPU/crash-handler
	// children, or games holding the overlay DLL, can still keep files in
	// the current version's folder mapped, which makes the promotion
	// rename fail. Draining is only worth the wait when there is a build
	// to promote and the main process is actually gone; otherwise
	// tryLaunchCurrent skips promotion on its own.
	if build := mv.GetCurrentVersion(); err == nil && mv.HasReadyPending() && build != nil {
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer drainCancel()
		err = setup.WaitForDirQuiescence(drainCtx, nwin.ListProcessModulePaths, build.Path, os.Getpid())
		if err != nil {
			// still launch: the user asked for a restart, and promotion
			// failing just relaunches the version they were already on
			log.Printf("%+v", err)
			log.Printf("Promotion will likely fail; launching whatever is current")
		}
	}

	err = nc.tryLaunchCurrent(mv, nil)
	if errors.Is(err, errLaunchAsUser) {
		log.Printf("%+v", err)
		setup.EnableJSON()
		setup.Emit(setup.Log{Level: "error", Message: fmt.Sprintf("%+v", err)})
		setup.DisableJSON()

		msg := fmt.Sprintf("The update to %s was installed, but the app could not be started automatically.\nPlease start %s again from the Start menu.", nc.cli.AppName, nc.cli.AppName)
		walk.MsgBox(nil, "Update installed", msg, walk.MsgBoxOK|walk.MsgBoxIconInformation)
		return nil
	}
	if err != nil {
		nc.ErrorDialog(err)
	}

	return nil
}

func (nc *nativeCore) Uninstall() error {
	log.Printf("Uninstall was requested...")

	// our stable working directory is the install root itself, which
	// would block removing it below
	if err := os.Chdir(os.TempDir()); err != nil {
		log.Printf("Could not leave install directory: %v", err)
	}

	mv, err := nc.newMultiverse()
	if err != nil {
		return err
	}

	cli := nc.cli

	pathsToKill := []string{}
	currentBuild := mv.GetCurrentVersion()
	if currentBuild != nil {
		pathsToKill = append(pathsToKill, filepath.Join(currentBuild.Path, nc.exeName()))
	}

	err = nwin.KillAll(pathsToKill)
	if err != nil {
		log.Printf("While killing processes: %+v", err)
	}

	warn := func(err error) {
		log.Printf("warning: %+v", err)
		log.Printf("(continuing anyway)")
	}

	for _, spec := range nc.shortcutSpecs() {
		log.Printf("remove (%s)", spec.Path)
		err = os.Remove(spec.Path)
		if err != nil {
			warn(err)
		}
	}

	cleanBaseDir := func() error {
		dir, err := os.Open(nc.baseDir)
		if err != nil {
			if os.IsNotExist(err) {
				// good!
				return nil
			}
		}
		defer dir.Close()

		names, err := dir.Readdirnames(-1)
		if err != nil {
			return err
		}

		// N.B: we can't remove `itch-setup.exe`, because
		// it is us! and we are currently running!
		deleteMap := map[string]bool{
			// app icon
			"app.ico": true,
			// weird UWP stuff, Windows 10 visual tile styles, ugh
			nc.visualElementsManifestName(): true,
			// installed version state
			"state.json": true,
			// cross-process state transition lock
			"state.lock": true,
		}

		for _, name := range names {
			fullPath := filepath.Join(nc.baseDir, name)

			if deleteMap[name] {
				log.Printf("delete (%s)", fullPath)
				err := os.Remove(fullPath)
				if err != nil {
					warn(err)
				}
			} else if strings.HasPrefix(name, "app-") {
				tries := 3

				for {
					log.Printf("delete (%s)/", fullPath)
					err := os.RemoveAll(fullPath)
					if err != nil {
						if tries > 0 {
							log.Printf("retrying in 1 second...")
							time.Sleep(1 * time.Second)
							tries--
							continue
						}
						warn(err)
					}
					break
				}

			} else {
				log.Printf("keep (%s)", fullPath)
			}
		}
		return nil
	}

	err = cleanBaseDir()
	if err != nil {
		nc.ErrorDialog(err)
	}

	log.Printf("Removing uninstaller entry...")
	err = nwin.RemoveUninstallerRegistryKey(cli, nc.user)
	if err != nil {
		log.Printf("While removing uninstaller entry: %+v", err)
		log.Printf("(Note: these aren't critical)")
	}

	renameSelfToTrash := func() error {
		log.Printf("Renaming self to temp directory...")
		trashPath := filepath.Join(os.TempDir(), ".itch-setup-trash")
		err := os.MkdirAll(trashPath, 0755)
		if err != nil {
			return err
		}

		selfPath := filepath.Join(nc.baseDir, "itch-setup.exe")

		selfTrashPath := filepath.Join(trashPath, "itch-setup.exe")
		log.Printf("We'll leave file at (%s), best we can do, sorry :(", selfTrashPath)
		err = os.Rename(selfPath, selfTrashPath)
		if err != nil {
			return err
		}

		return nil
	}

	err = renameSelfToTrash()
	if err != nil {
		warn(err)
	} else {
		log.Printf("Attempting to remove folder (will fail if we've kept files)")
		err := os.Remove(nc.baseDir)
		if err != nil {
			log.Printf("Yup, it's staying")
		} else {
			log.Printf("Ooh, clean uninstall. Neat!")
		}
	}

	// Clean app components from user data directory
	setup.CleanUserDataDir(nc.userDataPath(), warn)

	log.Printf("%s is uninstalled.", nc.cli.AppName)
	log.Printf("")
	log.Printf("Note: User data preserved in %%AppData%%\\%s", nc.cli.AppName)
	log.Printf("(contains: users/, preferences.json, config.json, db/)")
	log.Printf("")

	return nil
}

type onSuccessFunc func()

func (nc *nativeCore) killAllPrevious() {
	log.Printf("Attempting to kill all old instances of %s", nc.cli.AppName)
	var pathsToKill []string

	log.Printf("Scanning (%s)", nc.baseDir)
	appDirs, err := readdirnames(nc.baseDir)
	if err != nil {
		log.Printf("Skipping, could not list app dirs: %+v", err)
		return
	}

	var exeName = nc.exeName()
	log.Printf("Will look for a file named (%s)", exeName)

	scanAppDir := func(appDir string) error {
		absoluteAppDir := filepath.Join(nc.baseDir, appDir)
		log.Printf("Scanning (%s)", absoluteAppDir)

		if !strings.HasPrefix(appDir, "app-") {
			return nil
		}

		appFiles, err := readdirnames(absoluteAppDir)
		if err != nil {
			return err
		}

		for _, appFile := range appFiles {
			if appFile == exeName {
				absoluteAppFile := filepath.Join(absoluteAppDir, appFile)
				log.Printf("Adding (%s) to kill list", absoluteAppFile)
				pathsToKill = append(pathsToKill, absoluteAppFile)
			}
		}
		return nil
	}

	for _, appDir := range appDirs {
		err := scanAppDir(appDir)
		if err != nil {
			log.Printf("Skipping dir (%s): %+v", appDir, err)
			continue
		}
	}

	log.Printf("%d paths in kill list", len(pathsToKill))

	err = nwin.KillAll(pathsToKill)
	if err != nil {
		log.Printf("While killing old instances: %+v", err)
	}
}

func readdirnames(name string) ([]string, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return f.Readdirnames(0) // all dirs, please
}

// appCommand prepares a command to launch the installed app with a
// stable working directory: passing down a versioned directory would
// block future update promotions from renaming it.
func (nc *nativeCore) appCommand(exePath string, args ...string) *exec.Cmd {
	cmd := exec.Command(exePath, args...)
	cmd.Dir = nc.baseDir
	return cmd
}

// The desktop-user launch is only for elevation we asked for: users who
// are elevated on their own (UAC off, or running as administrator on
// purpose) keep the plain launch. When it fails there is no fallback to
// our administrator token, since that would elevate butler, every game
// and every file the app writes for the whole session; the update is
// already applied, so the next normal launch picks it up.
func (nc *nativeCore) startApp(exePath string, args []string) error {
	if nc.cli.Elevate && nwin.IsElevated() {
		log.Printf("Running elevated, launching app as desktop user")
		err := nwin.StartProcessAsDesktopUser(exePath, args, nc.baseDir)
		if err != nil {
			return fmt.Errorf("%w: %v", errLaunchAsUser, err)
		}
		return nil
	}
	return nc.appCommand(exePath, args...).Start()
}

var errLaunchAsUser = fmt.Errorf("could not launch as the desktop user")

// canPromoteSafely returns false if some running process holds files
// inside the current version's folder, which would make the promotion
// rename fail (and needlessly stall on rename retries).
func (nc *nativeCore) canPromoteSafely(mv setup.Multiverse) bool {
	build := mv.GetCurrentVersion()
	if build == nil {
		return true
	}

	blockers, err := setup.FindBlockingProcesses(nwin.ListProcessModulePaths, build.Path, os.Getpid())
	if err != nil {
		log.Printf("Could not check for processes using (%s): %+v", build.Path, err)
		// don't block promotion on enumeration problems; the rename has
		// its own retries
		return true
	}

	for _, b := range blockers {
		log.Printf("Current version is in use by PID %d (%s)", b.PID, strings.Join(b.Paths, ", "))
	}
	return len(blockers) == 0
}

// returns true if it successfully launched
func (nc *nativeCore) tryLaunchCurrent(mv setup.Multiverse, onSuccess onSuccessFunc) error {
	didPromoteReady := false
	if mv.HasReadyPending() {
		if !nc.canPromoteSafely(mv) {
			log.Printf("Has ready pending, but current version is in use; skipping promotion")
		} else {
			log.Printf("Has ready pending, trying to make it current...")
			err := mv.MakeReadyCurrent()
			if err != nil {
				log.Printf("Could not make ready current: %+v", err)
			} else {
				didPromoteReady = true
			}
		}
	}

	build := mv.GetCurrentVersion()
	if build == nil {
		return nil
	}

	if didPromoteReady {
		log.Printf("Ready build is now current, syncing uninstall registry metadata to (%s)", build.Version)
		nc.syncUninstallRegistryEntry(build.Version)
	}

	log.Printf("Launching (%s) from (%s)", build.Version, build.Path)

	err := nc.startApp(filepath.Join(build.Path, nc.exeName()), nc.cli.Args)
	if err != nil {
		return fmt.Errorf("encountered a problem while launching %s: %w", nc.cli.AppName, err)
	}

	if onSuccess != nil {
		onSuccess()
	}

	log.Printf("App launched, getting out of the way")
	os.Exit(0)

	// unreachable, but go's compiler doesn't know it
	return nil
}

func (nc *nativeCore) showInstallGUI() error {
	cli := nc.cli

	var installer *setup.Installer

	var trayIcon *walk.NotifyIcon
	var installDirLineEdit *walk.LineEdit
	var pb *walk.ProgressBar
	var progressLabel *walk.Label
	var imageView *walk.ImageView
	var progressComposite, optionsComposite *walk.Composite

	installDir := nc.baseDir

	kickoffInstall := func() {
		kickErr := func() error {
			nc.baseDir = installDir

			mv, err := nc.newMultiverse()
			if err != nil {
				return err
			}
			installer.Install(mv)

			return nil
		}()
		if kickErr != nil {
			nc.ErrorDialog(fmt.Errorf("Error during installation: %w", kickErr))
		}
	}

	pickInstallLocation := func() {
		dlg := new(walk.FileDialog)

		dlg.Title = cli.Localizer.T("setup.tooltip.location")
		dlg.FilePath = installDir

		if ok, err := dlg.ShowBrowseFolder(nc.mainWindow); err != nil {
			log.Println(fmt.Sprintf("ShowBrowserFolder error: %s", err.Error()))
		} else if !ok {
			// nothing picked
		} else {
			if nc.ensureWritable(dlg.FilePath, installDirLineEdit) {
				installDir = dlg.FilePath
				installDirLineEdit.SetText(installDir)
			}
		}
	}

	imageWidth := 622
	imageHeight := 301

	controlsHeight := 120
	windowHeight := imageHeight + 158 // found by trial & error

	windowSize := ui.Size{
		Width:  imageWidth,
		Height: windowHeight,
	}

	baseTitle := cli.Localizer.T("setup.window.title", map[string]string{"app_name": cli.AppName})

	onTriggerInstall := func() {
		installDir = strings.TrimSpace(installDirLineEdit.Text())
		if !nc.ensureWritable(installDir, installDirLineEdit) {
			return
		}

		progressComposite.SetVisible(true)
		optionsComposite.SetVisible(false)

		go kickoffInstall()
	}

	err := ui.MainWindow{
		Title:   baseTitle,
		MinSize: windowSize,
		MaxSize: windowSize,
		Size:    windowSize,
		Layout: ui.VBox{
			MarginsZero: true,
			SpacingZero: true,
		},
		Children: []ui.Widget{
			ui.ImageView{
				AssignTo: &imageView,
				MinSize:  ui.Size{Width: imageWidth, Height: imageHeight},
				MaxSize:  ui.Size{Width: imageWidth, Height: imageHeight},
			},
			ui.Composite{
				MinSize: ui.Size{Height: controlsHeight},
				Layout: ui.VBox{
					Margins: ui.Margins{
						Left:  30,
						Right: 30,
					},
				},
				Children: []ui.Widget{
					ui.VSpacer{},
					ui.Label{
						Text: cli.Localizer.T("setup.window.welcome", map[string]string{"app_name": cli.AppName}),
					},
					ui.VSpacer{},
					ui.Composite{
						Layout: ui.HBox{
							MarginsZero: true,
						},
						Children: []ui.Widget{
							ui.PushButton{
								Text: cli.Localizer.T("setup.action.browse"),
								OnClicked: func() {
									pickInstallLocation()
								},
							},
							ui.LineEdit{
								AssignTo:    &installDirLineEdit,
								Text:        installDir,
								ToolTipText: cli.Localizer.T("setup.tooltip.location"),
								OnKeyPress: func(key walk.Key) {
									if key == walk.KeyReturn {
										onTriggerInstall()
									}
								},
							},
							ui.PushButton{
								Text:      cli.Localizer.T("setup.action.install"),
								OnClicked: onTriggerInstall,
							},
						},
					},
					ui.VSpacer{},
				},
				AssignTo: &optionsComposite,
			},
			ui.Composite{
				MinSize: ui.Size{Height: controlsHeight},
				Layout: ui.VBox{
					Margins: ui.Margins{
						Left:  30,
						Right: 30,
					},
				},
				Children: []ui.Widget{
					ui.VSpacer{},
					ui.ProgressBar{
						MinValue: 0,
						MaxValue: 1000,
						AssignTo: &pb,
					},
					ui.VSpacer{Size: 10},
					ui.Composite{
						Layout: ui.HBox{},
						Children: []ui.Widget{
							ui.Label{
								Text:          cli.Localizer.T("setup.status.preparing"),
								AssignTo:      &progressLabel,
								TextAlignment: ui.AlignCenter,
							},
						},
					},
					ui.VSpacer{},
				},
				Visible:  false,
				AssignTo: &progressComposite,
			},
		},
		AssignTo: &nc.mainWindow,
		OnSizeChanged: func() {
			if nc.mainWindow == nil {
				return
			}
			// this is kinda crap UX, but resizing the window is really ugly
			nc.mainWindow.SetSize(walk.Size{Width: windowSize.Width, Height: windowSize.Height})
		},
	}.Create()
	if err != nil {
		log.Fatal(err)
	}

	// remove maximize button
	style := win.GetWindowLong(nc.mainWindow.Handle(), win.GWL_STYLE)
	style &^= win.WS_MAXIMIZEBOX
	// style &^= win.WS_THICKFRAME
	win.SetWindowLong(nc.mainWindow.Handle(), win.GWL_STYLE, style)

	trayIcon, err = walk.NewNotifyIcon(nc.mainWindow)
	if err != nil {
		log.Fatal(err)
	}

	// see itch-setup.rc
	iconID := 101
	if cli.AppName == "kitch" {
		iconID = 102
	}

	ic, err := walk.NewIconFromResourceId(iconID)
	if err != nil {
		log.Println("Could not load icon, oh well")
	} else {
		trayIcon.SetIcon(ic)
		nc.mainWindow.SetIcon(ic)
	}

	err = trayIcon.SetVisible(true)
	if err != nil {
		log.Fatal(err)
	}

	trayIcon.SetToolTip(cli.Localizer.T("setup.window.title"))
	if err != nil {
		log.Fatal(err)
	}

	nwin.SetInstallerImage(cli, imageView)

	installer = setup.NewInstaller(setup.InstallerSettings{
		Localizer:  cli.Localizer,
		AppName:    cli.AppName,
		NoFallback: cli.NoFallback,
		OnError: func(err error) {
			nc.mainWindow.Synchronize(func() {
				nc.ErrorDialog(fmt.Errorf("Error during warm-up: %w", err))
			})
		},
		OnProgressLabel: func(label string) {
			nc.mainWindow.Synchronize(func() {
				progressLabel.SetText(label)
			})
		},
		OnProgress: func(progress float64) {
			nc.mainWindow.Synchronize(func() {
				pb.SetValue(int(progress * 1000.0))
			})
		},
		OnSource: func(sourceIn setup.InstallSource) {
			nc.mainWindow.Synchronize(func() {
				nc.mainWindow.SetTitle(fmt.Sprintf("%s - %s", baseTitle, sourceIn.Version))
			})

			if nc.cli.Silent {
				log.Printf("In silent mode, kicking off installation now...")
				go kickoffInstall()
			}
		},
		OnFinish: func(source setup.InstallSource) {
			nc.mainWindow.Synchronize(func() {
				mv, err := nc.newMultiverse()
				if err != nil {
					nc.ErrorDialog(err)
				}

				err = nc.doPostInstall(mv, PostInstallParams{
					ForUpgrade: false,
				})
				if err != nil {
					nc.ErrorDialog(err)
				}

				nc.killAllPrevious()

				err = nc.tryLaunchCurrent(mv, func() {
					trayIcon.ShowInfo(cli.AppName, fmt.Sprintf("The installation went well, %s is now starting up!", cli.AppName))
				})
				if err != nil {
					nc.ErrorDialog(err)
				}
			})
		},
	})
	installer.WarmUp()

	nwin.CenterWindow(nc.mainWindow.AsFormBase())

	if nc.cli.Silent {
		nc.mainWindow.SetVisible(false)
	}
	nc.mainWindow.Run()

	return nil
}

func (nc *nativeCore) ErrorDialog(errShown error) {
	cli := nc.cli

	var dlg *walk.Dialog

	log.Printf("Fatal error: %+v", errShown)

	buf := new(bytes.Buffer)
	fmt.Fprintf(buf, `%s`, cli.Localizer.T("setup.error_dialog.title"))
	buf.WriteString("\n\n")
	fmt.Fprintf(buf, `%s-setup, %s`, cli.AppName, cli.VersionString)
	buf.WriteString("\n\n")
	fmt.Fprintf(buf, "%+v", errShown)

	var te *walk.TextEdit

	var err error
	dlgDecl := ui.Dialog{
		Title:    cli.Localizer.T("setup.error_dialog.title"),
		MinSize:  ui.Size{Width: 600, Height: 400},
		Layout:   ui.VBox{},
		AssignTo: &dlg,
		Children: []ui.Widget{
			ui.TextEdit{
				Text:          strings.ReplaceAll(buf.String(), "\n", "\r\n"),
				StretchFactor: 2,
				ReadOnly:      true,
				VScroll:       true,
				MaxSize: ui.Size{
					Width:  0,
					Height: 600,
				},
				AssignTo: &te,
			},
			ui.Composite{
				Layout: ui.HBox{
					MarginsZero: true,
				},
				Children: []ui.Widget{
					ui.LinkLabel{
						Text: `<a href="https://github.com/itchio/itch/issues">Open issue tracker</a>`,
						OnLinkActivated: func(link *walk.LinkLabelLink) {
							open.Start(link.URL())
						},
					},
					ui.HSpacer{},
				},
			},
			ui.VSpacer{Size: 10},
			ui.Composite{
				Layout: ui.HBox{
					MarginsZero: true,
				},
				Children: []ui.Widget{
					ui.HSpacer{},
					ui.PushButton{
						Text: cli.Localizer.T("prompt.action.ok"),
						OnClicked: func() {
							dlg.Close(0)
						},
					},
					ui.HSpacer{},
				},
			},
		},
	}
	if nc.mainWindow == nil {
		// go's nil is misused by lxn/walk so we need this
		err = dlgDecl.Create(nil)
	} else {
		err = dlgDecl.Create(nc.mainWindow)
	}
	if err != nil {
		log.Printf("Error in dialog: %+v", err)
		os.Exit(1)
	}

	nwin.CenterWindow(dlg.AsFormBase())

	// cf. https://github.com/itchio/itch-setup/blob/922c8d02ecd01eebc2e920cc6b69aff64e0cc563/native/native_linux.go#L216-L241
	// If the start is –1, any current selection is deselected.
	te.SetTextSelection(-1, 0)

	dlg.Run()
	os.Exit(1)
}

type shortcutSpec struct {
	Path         string
	OnlyIfExists bool
}

func (nc *nativeCore) shortcutSpecs() []shortcutSpec {
	return []shortcutSpec{
		nc.desktopShortcutSpecs(),
		nc.startMenuShortcutSpecs(),
		nc.pinnedShortcutSpec(),
	}
}

func (nc *nativeCore) desktopShortcutSpecs() shortcutSpec {
	return shortcutSpec{
		Path: filepath.Join(nc.folders.Desktop, nc.shortcutName()),
	}
}

func (nc *nativeCore) startMenuShortcutSpecs() shortcutSpec {
	return shortcutSpec{
		Path: filepath.Join(nc.folders.Programs, "Itch Corp", nc.shortcutName()),
	}
}

func (nc *nativeCore) pinnedShortcutSpec() shortcutSpec {
	return shortcutSpec{
		// Yes, this is Windows 10 stuff.
		// No, I don't know either.
		Path: filepath.Join(nc.folders.RoamingAppData, "Microsoft", "Internet Explorer", "Quick Launch", "User Pinned", "TaskBar", nc.shortcutName()),

		// This shortcut only exists if the app was pinned to the task bar,
		// we don't want to create it ourselves.
		OnlyIfExists: true,
	}
}

func (nc *nativeCore) shortcutName() string {
	return fmt.Sprintf("%s.lnk", nc.cli.AppName)
}

func (nc *nativeCore) exeName() string {
	return fmt.Sprintf("%s.exe", nc.cli.AppName)
}

func (nc *nativeCore) RunGame(gameID int64) error {
	return rungame.Run(rungame.Params{
		AppName:     nc.cli.AppName,
		UserDataDir: nc.userDataPath(),
		ProfileID:   nc.cli.ProfileID,
		LaunchApp:   nc.launchAppDetached,
	}, gameID)
}

func (nc *nativeCore) SyncLauncher() error {
	comshim.Add(1)
	defer comshim.Done()

	mv, err := nc.newMultiverse()
	if err != nil {
		return err
	}
	if mv.GetCurrentVersion() == nil {
		log.Printf("No installed %s, nothing to sync", nc.cli.AppName)
		return nil
	}
	return nc.doPostInstall(mv, PostInstallParams{ForUpgrade: true})
}

// launchAppDetached starts the installed app without waiting for it and
// without exiting the process, unlike tryLaunchCurrent.
func (nc *nativeCore) launchAppDetached(appArgs []string, extraEnv []string) error {
	mv, err := nc.newMultiverse()
	if err != nil {
		return err
	}

	if mv.HasReadyPending() {
		if !nc.canPromoteSafely(mv) {
			log.Printf("Has ready pending, but current version is in use; skipping promotion")
		} else if err := mv.MakeReadyCurrent(); err != nil {
			log.Printf("Could not make ready current: %+v", err)
		} else if build := mv.GetCurrentVersion(); build != nil {
			nc.syncUninstallRegistryEntry(build.Version)
		}
	}

	build := mv.GetCurrentVersion()
	if build == nil {
		return fmt.Errorf("No valid version of %s found installed: %w", nc.cli.AppName, rungame.ErrAppNotInstalled)
	}

	exePath := filepath.Join(build.Path, nc.exeName())
	if _, err := os.Stat(exePath); err != nil {
		// the multiverse state can outlive the actual files
		return fmt.Errorf("missing app executable (%s): %w", exePath, rungame.ErrAppNotInstalled)
	}
	log.Printf("Launching (%s) from (%s)", build.Version, exePath)
	cmd := nc.appCommand(exePath, append(appArgs, nc.cli.Args...)...)
	cmd.Env = append(rungame.EnvWithoutOverlayPreload(), extraEnv...)
	return cmd.Start()
}

func (nc *nativeCore) newMultiverse() (setup.Multiverse, error) {
	return setup.NewMultiverse(&setup.MultiverseParams{
		AppName: nc.cli.AppName,
		BaseDir: nc.baseDir,
	})
}

func (nc *nativeCore) userDataPath() string {
	return filepath.Join(nc.folders.RoamingAppData, nc.cli.AppName)
}

func (nc *nativeCore) visualElementsManifestName() string {
	return "itch-setup.VisualElementsManifest.xml"
}

func (nc *nativeCore) visualElementsManifestPath() string {
	return filepath.Join(nc.baseDir, nc.visualElementsManifestName())
}

func (nc *nativeCore) writeVisualElementsManifest() error {
	manifestContents := `<Application xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <VisualElements
    BackgroundColor="#2E2B2C"
    ShowNameOnSquare150x150Logo="on"
    ForegroundText="light"/>
</Application>`

	manifestPath := nc.visualElementsManifestPath()

	log.Printf("Writing visual elements manifest (%s)", manifestPath)
	err := os.WriteFile(manifestPath, []byte(manifestContents), 0644)
	if err != nil {
		return err
	}

	return nil
}

func (nc *nativeCore) isValidInstallDir(dir string) bool {
	return filepath.IsAbs(dir)
}

func (nc *nativeCore) isWritableInstallDir(dir string) bool {
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return false
	}

	testFile := filepath.Join(dir, ".write-test")
	contents := []byte("not program files please")
	err = os.WriteFile(testFile, contents, 0644)
	if err != nil {
		return false
	}

	os.Remove(testFile)
	return true
}

func (nc *nativeCore) ensureWritable(dir string, installDirLineEdit *walk.LineEdit) bool {
	if dir == "" {
		installDirLineEdit.SetText(nc.baseDir)
		msg := "Please choose a non-empty install location.\nThe install location has been reset to the default."
		walk.MsgBox(nc.mainWindow, "Error", msg, walk.MsgBoxOK)
		return false
	}

	if !nc.isValidInstallDir(dir) {
		installDirLineEdit.SetText(nc.baseDir)
		msg := fmt.Sprintf("\"%s\" is not a valid install location.\nThe install location has been reset to the default.", dir)
		walk.MsgBox(nc.mainWindow, "Error", msg, walk.MsgBoxOK)
		return false
	}

	if !nc.isWritableInstallDir(dir) {
		installDirLineEdit.SetText(nc.baseDir)
		msg := fmt.Sprintf("You do not have permission to install to folder \"%s\".\nThe install location has been reset to the default.", dir)
		walk.MsgBox(nc.mainWindow, "Error", msg, walk.MsgBoxOK)
		return false
	}

	// The check above ran with our own token. If we're elevated it says
	// nothing about the account that will run updates later.
	if nwin.IsElevated() {
		userCanWrite, err := nwin.DesktopUserCanWrite(dir)
		if err != nil {
			log.Printf("Could not check whether the desktop user can write to (%s): %+v", dir, err)
		} else if !userCanWrite {
			msg := fmt.Sprintf("Your user account cannot write to \"%s\".\n\n%s will install there, but every update will ask for administrator approval.\n\nInstall here anyway?", dir, nc.cli.AppName)
			if walk.MsgBox(nc.mainWindow, "Install location", msg, walk.MsgBoxYesNo|walk.MsgBoxIconWarning) != walk.DlgCmdYes {
				installDirLineEdit.SetText(nc.baseDir)
				return false
			}
		}
	}

	return true
}

func (nc *nativeCore) Info() {
	log.Printf("We are on Windows, our folders are:")
	log.Printf("Desktop: %s", nc.folders.Desktop)
	log.Printf("LocalAppData: %s", nc.folders.LocalAppData)
	log.Printf("RoamingAppData: %s", nc.folders.RoamingAppData)
	log.Printf("Programs: %s", nc.folders.Programs)
}
