package native

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/errors"
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

func NewNativeCore(cli cl.CLI) (NativeCore, error) {
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
		log.Printf("Default base dir:  %s", defaultBaseDir)
		log.Printf("Registry base dir: %s", registryBaseDir)
		if defaultBaseDir == registryBaseDir {
			log.Printf("Same as default, moving on")
		} else {
			log.Printf("Strays from defaults, taking it into account")
			baseDir = defaultBaseDir
		}
	}
	log.Printf("Initial base dir: %s", baseDir)
	nc.baseDir = baseDir

	return nc, nil
}

func (nc *nativeCore) Install() error {
	cli := nc.cli

	if cli.PreferLaunch {
		log.Printf("Launch preferred, looking for a valid app folder")
		err := nc.tryLaunchCurrent(nil)
		if err != nil {
			log.Printf("While launching current: %+v", err)
			log.Printf("Continuing with setup")
		}
	}

	log.Println("Showing install GUI")
	return nc.showInstallGUI()
}

func (nc *nativeCore) Upgrade() error {
	return errors.Errorf("Upgrade: stub!")
}

func (nc *nativeCore) Relaunch() error {
	cli := nc.cli

	mv, err := nc.newMultiverse()
	if err != nil {
		return err
	}

	ctx, _ := context.WithTimeout(context.Background(), 60*time.Second)
	setup.WaitForProcessToExit(ctx, cli.RelaunchPID)

	if mv.HasReadyPending() {
		log.Printf("Has ready pending, trying to make it current...")
		err := mv.MakeReadyCurrent()
		if err != nil {
			log.Printf("Could not make ready current: %+v", err)
		}
	}

	err = nc.tryLaunchCurrent(nil)
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

	log.Println("Removing desktop shortcut...")
	err = os.Remove(nc.shortcutPath())
	if err != nil {
		log.Println("While removing full shortcut", err.Error())
		log.Println("(Note: shortcut errors aren't critical)")
	}

	// FIXME: this should be a method of mv - it knows what to remove
	// and what not to remove

	log.Println("Nuking app folder")
	tries := 5
	for i := 0; i < tries; i++ {
		err = os.RemoveAll(nc.baseDir)
		if err != nil {
			log.Printf("%+v", err)
			log.Printf("Sleeping a bit then retrying")
			time.Sleep(1 * time.Second)
			continue
		}
		break
	}

	log.Println("Removing uninstaller entry...")
	err = nwin.RemoveUninstallerRegistryKey(cli)
	if err != nil {
		log.Println("While removing uninstaller entry", err.Error())
		log.Println("(Note: these aren't critical)")
	}
	return nil
}

type onSuccessFunc func()

// returns true if it successfully launched
func (nc *nativeCore) tryLaunchCurrent(onSuccess onSuccessFunc) error {
	cli := nc.cli

	mv, err := nc.newMultiverse()
	if err != nil {
		return err
	}

	build := mv.GetCurrentVersion()
	if build == nil {
		return nil
	}

	log.Printf("Launching (%s) from (%s)", build.Version, build.Path)

	cmd := exec.Command(filepath.Join(build.Path, nc.exeName()))

	err = cmd.Start()
	if err != nil {
		prettyErr := errors.WithMessage(err, fmt.Sprintf("Encountered a problem while launching %s", cli.AppName))
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

	installDirIn := nc.baseDir

	var installer *setup.Installer

	var trayIcon *walk.NotifyIcon
	var installDirLabel *walk.LineEdit
	var pb *walk.ProgressBar
	var progressLabel *walk.Label
	var imageView *walk.ImageView
	var progressComposite, optionsComposite *walk.Composite

	installDir := installDirIn

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
			installDir = dlg.FilePath
			installDirLabel.SetText(installDir)
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
							ui.LineEdit{
								AssignTo:    &installDirLabel,
								Text:        installDir,
								ReadOnly:    true,
								ToolTipText: cli.Localizer.T("setup.tooltip.location"),
								OnMouseUp: func(x, y int, button walk.MouseButton) {
									pickInstallLocation()
								},
							},
							ui.PushButton{
								Text: cli.Localizer.T("setup.action.install"),
								OnClicked: func() {
									progressComposite.SetVisible(true)
									optionsComposite.SetVisible(false)

									go kickoffInstall()
								},
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
					ui.Label{
						Text:     cli.Localizer.T("setup.status.preparing"),
						AssignTo: &progressLabel,
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

	// remove maximize button
	style := win.GetWindowLong(nc.mainWindow.Handle(), win.GWL_STYLE)
	style &^= win.WS_MAXIMIZEBOX
	// style &^= win.WS_THICKFRAME
	win.SetWindowLong(nc.mainWindow.Handle(), win.GWL_STYLE, style)

	if err != nil {
		log.Fatal(err)
	}

	trayIcon, err = walk.NewNotifyIcon()
	if err != nil {
		log.Fatal(err)
	}

	// see itch-setup.rc
	iconId := 101
	if cli.AppName == "kitch" {
		iconId = 102
	}

	ic, err := walk.NewIconFromResourceId(iconId)
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
				setupLocalPath, err := nwin.CopySelf(installDir)
				if err != nil {
					nc.ErrorDialog(err)
				}

				// this creates $installDir/app.ico
				err = nwin.CreateUninstallRegistryEntry(cli, installDir, source)
				if err != nil {
					log.Printf("While creating registry entry: %s", err.Error())
				}

				shortcutArguments := fmt.Sprintf("--prefer-launch --appname %s", cli.AppName)

				err = nwin.CreateShortcut(nwin.ShortcutSettings{
					ShortcutFilePath: nc.shortcutPath(),
					TargetPath:       setupLocalPath,
					Arguments:        shortcutArguments,
					Description:      "The best way to play your itch.io games",
					IconLocation:     filepath.Join(installDir, "app.ico"),
					WorkingDirectory: filepath.Join(installDir),
				})

				if err != nil {
					nc.ErrorDialog(errors.WithMessage(err, "While creating shortcut marker"))
				}

				err = nc.tryLaunchCurrent(func() {
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

	res := dlg.Run()
	log.Printf("Dialog res: %#v\n", res)

	os.Exit(1)
}

func (nc *nativeCore) shortcutPath() string {
	return filepath.Join(nc.folders.Desktop, fmt.Sprintf("%s.lnk", nc.cli.AppName))
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
