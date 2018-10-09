package native

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
	"github.com/scjalliance/comshim"
	"github.com/skratchdot/open-golang/open"

	"github.com/itchio/itch-setup/cl"
	"github.com/itchio/itch-setup/native/nwin"
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
}

// NewCore returns a windows-specific Core implementation
func NewCore(cli cl.CLI) (Core, error) {
	nc := &nativeCore{cli: cli}

	folders, err := nwin.GetFolders()
	if err != nil {
		return nil, errors.WithMessage(err, "During setup initialization")
	}

	nc.folders = folders

	defaultBaseDir := filepath.Join(folders.LocalAppData, cli.AppName)
	baseDir := defaultBaseDir

	registryBaseDir, err := nwin.GetRegistryInstallDir(cli)
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
	log.Printf("Initial base dir: (%s)", baseDir)
	nc.baseDir = baseDir

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

	log.Println("Showing install GUI")
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
		Localizer: cli.Localizer,
		AppName:   cli.AppName,
	})
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

type PostInstallParams struct {
	ForUpgrade bool
}

func (nc *nativeCore) doPostInstall(mv setup.Multiverse, params PostInstallParams) error {
	installDir := nc.baseDir
	cli := nc.cli

	currentBuild := mv.GetCurrentVersion()
	if currentBuild == nil {
		return errors.Errorf("internal error (in post-install with a nil currentBuild)")
	}

	setupLocalPath, err := nwin.CopySelf(installDir)
	if err != nil {
		nc.ErrorDialog(err)
	}

	// this creates $installDir/app.ico
	err = nwin.CreateUninstallRegistryEntry(cli, installDir, currentBuild.Version)
	if err != nil {
		log.Printf("While creating registry entry: %s", err.Error())
	}

	// this needs to be done before the shortcut is created
	err = nc.writeVisualElementsManifest()
	if err != nil {
		return err
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
		})
		if err != nil {
			nc.ErrorDialog(errors.WithMessage(err, "while creating shortcut"))
		}
	}

	return nil
}

func (nc *nativeCore) Relaunch() error {
	cli := nc.cli

	mv, err := nc.newMultiverse()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	setup.WaitForProcessToExit(ctx, cli.RelaunchPID)

	err = nc.tryLaunchCurrent(mv, nil)
	if err != nil {
		nc.ErrorDialog(err)
	}

	return nil
}

func (nc *nativeCore) Uninstall() error {
	log.Println("Uninstall was requested...")
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
		log.Println("While killing processes", err.Error())
	}

	warn := func(err error) {
		log.Printf("warning: %v", err)
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

	log.Println("Removing uninstaller entry...")
	err = nwin.RemoveUninstallerRegistryKey(cli)
	if err != nil {
		log.Println("While removing uninstaller entry", err.Error())
		log.Println("(Note: these aren't critical)")
	}

	renameSelfToTrash := func() error {
		log.Println("Renaming self to temp directory...")
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

	return nil
}

type onSuccessFunc func()

// returns true if it successfully launched
func (nc *nativeCore) tryLaunchCurrent(mv setup.Multiverse, onSuccess onSuccessFunc) error {
	if mv.HasReadyPending() {
		log.Printf("Has ready pending, trying to make it current...")
		err := mv.MakeReadyCurrent()
		if err != nil {
			log.Printf("Could not make ready current: %+v", err)
		}
	}

	build := mv.GetCurrentVersion()
	if build == nil {
		return nil
	}

	log.Printf("Launching (%s) from (%s)", build.Version, build.Path)

	cmd := exec.Command(filepath.Join(build.Path, nc.exeName()), nc.cli.Args...)

	err := cmd.Start()
	if err != nil {
		prettyErr := errors.WithMessage(err, fmt.Sprintf("Encountered a problem while launching %s", nc.cli.AppName))
		nc.ErrorDialog(prettyErr)
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
			nc.ErrorDialog(errors.WithMessage(kickErr, "Error during installation"))
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
							ui.HSpacer{},
							ui.Label{
								Text:     cli.Localizer.T("setup.status.preparing"),
								AssignTo: &progressLabel,
							},
							ui.HSpacer{},
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

	trayIcon, err = walk.NewNotifyIcon()
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
		Localizer: cli.Localizer,
		AppName:   cli.AppName,
		OnError: func(err error) {
			nc.mainWindow.Synchronize(func() {
				nc.ErrorDialog(errors.WithMessage(err, "Error during warm-up"))
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
				Text:          strings.Replace(buf.String(), "\n", "\r\n", -1),
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

func (nc *nativeCore) newMultiverse() (setup.Multiverse, error) {
	return setup.NewMultiverse(&setup.MultiverseParams{
		AppName: nc.cli.AppName,
		BaseDir: nc.baseDir,
	})
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
	err := ioutil.WriteFile(manifestPath, []byte(manifestContents), 0644)
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
	err = ioutil.WriteFile(testFile, contents, 0644)
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

	return true
}
