package native

import (
	"bytes"
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

var mw *walk.MainWindow = nil
var folders nwin.Folders

func Do(cli cl.CLI) {
	var err error

	folders, err = nwin.GetFolders()
	if err != nil {
		showError(cli, "Error during setup initialization: %+v", err)
	}

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

	mv, err := setup.NewMultiverse(&setup.MultiverseParams{
		AppName: cli.AppName,
		BaseDir: baseDir,
	})
	if err != nil {
		showError(cli, "Error during setup initialization: %+v", err)
	}

	if cli.Uninstall {
		err = doUninstall(cli, mv)
		if err != nil {
			showError(cli, "Error during uninstallation: %+v", err)
		}
		return
	}

	if cli.Relaunch {
		err = doRelaunch(cli, mv)
		if err != nil {
			showError(cli, "Error during relaunch: %+v", err)
		}
		return
	}

	if cli.PreferLaunch {
		log.Printf("Launch preferred, looking for a valid app folder")
		if appDir, ok := mv.GetValidAppDir(); ok {
			tryLaunch(cli, appDir)
		} else {
			log.Printf("No valid app folder found, continuing with installation")
		}
	}

	log.Println("Showing install GUI")
	showInstallGUI(cli, baseDir)
}

func doUninstall(cli cl.CLI, mv setup.Multiverse) error {
	log.Println("Uninstall was requested...")

	if !mv.IsValid() {
		log.Println("No valid install folder found, quitting.")
		return nil
	}

	pathsToKill := []string{}
	for _, appDir := range mv.ListAppDirs() {
		pathsToKill = append(pathsToKill, filepath.Join(appDir, exeName(cli)))
	}

	err := nwin.KillAll(pathsToKill)
	if err != nil {
		log.Println("While killing processes", err.Error())
	}

	log.Println("Removing shortcut...")
	err = os.Remove(shortcutPath(cli))
	if err != nil {
		log.Println("While removing full shortcut", err.Error())
		log.Println("(Note: shortcut errors aren't critical)")
	}

	log.Println("Nuking app folder")
	tries := 5
	for i := 0; i < tries; i++ {
		err = os.RemoveAll(mv.GetBaseDir())
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

func doRelaunch(cli cl.CLI, mv setup.Multiverse) error {
	if cli.RelaunchPID <= 0 {
		err := errors.Errorf("Relaunch cannot wait for invalid PID %d", cli.RelaunchPID)
		showError(cli, "%+v", err)
	}

	for tries := 10; tries > 0; tries-- {
		log.Printf("Making sure PID %d has exited (%d tries left)", cli.RelaunchPID, tries)

		proc, err := os.FindProcess(cli.RelaunchPID)
		if err == nil {
			log.Printf("PID %d still exists, sleeping for a bit...", cli.RelaunchPID)
			proc.Release()
			time.Sleep(1 * time.Second)
			continue
		}

		break
	}

	log.Printf("Done waiting for process to exit, looking for valid app dir...")

	appDir, ok := mv.GetValidAppDir()
	if ok {
		log.Printf("Found valid app dir, relaunching")
		tryLaunch(cli, appDir)
	} else {
		err := errors.Errorf("%s is not installed properly - could not find a valid version to launch", cli.AppName)
		showError(cli, "%+v", err)
	}

	return nil
}

func tryLaunch(cli cl.CLI, appDir string) {
	log.Println("Launching app")

	cmd := exec.Command(filepath.Join(appDir, exeName(cli)))

	err := cmd.Start()
	if err != nil {
		showError(cli, "Encountered a problem while launching %s: %s", cli.AppName, err.Error())
	}

	log.Printf("App launched, getting out of the way")
	os.Exit(0)
}

func showInstallGUI(cli cl.CLI, installDirIn string) {
	var installer *setup.Installer

	var ni *walk.NotifyIcon
	var installDirLabel *walk.LineEdit
	var pb *walk.ProgressBar
	var progressLabel *walk.Label
	var imageView *walk.ImageView
	var progressComposite, optionsComposite *walk.Composite

	installDir := installDirIn
	versionInstallDir := ""

	sourceChan := make(chan setup.InstallSource, 1)

	kickoffInstall := func() {
		kickErr := func() error {
			source := <-sourceChan

			versionInstallDir = filepath.Join(installDir, fmt.Sprintf("app-%s", source.Version))
			installer.Install(versionInstallDir)

			return nil
		}()
		if kickErr != nil {
			showError(cli, "Error during installation: %+v", kickErr)
		}
	}

	pickInstallLocation := func() {
		dlg := new(walk.FileDialog)

		dlg.Title = cli.Localizer.T("setup.tooltip.location")
		dlg.FilePath = installDir

		if ok, err := dlg.ShowBrowseFolder(mw); err != nil {
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
		AssignTo: &mw,
		OnSizeChanged: func() {
			if mw == nil {
				return
			}
			// this is kinda crap UX, but resizing the window is really ugly
			mw.SetSize(walk.Size{Width: windowSize.Width, Height: windowSize.Height})
		},
	}.Create()

	// remove maximize button
	style := win.GetWindowLong(mw.Handle(), win.GWL_STYLE)
	style &^= win.WS_MAXIMIZEBOX
	// style &^= win.WS_THICKFRAME
	win.SetWindowLong(mw.Handle(), win.GWL_STYLE, style)

	if err != nil {
		log.Fatal(err)
	}

	ni, err = walk.NewNotifyIcon()
	if err != nil {
		log.Fatal(err)
	}

	// see itch-setup.rc
	ic, err := walk.NewIconFromResourceId(101)
	if err != nil {
		log.Println("Could not load icon, oh well")
	} else {
		ni.SetIcon(ic)
		mw.SetIcon(ic)
	}

	nwin.SetInstallerImage(imageView)

	installer = setup.NewInstaller(setup.InstallerSettings{
		Localizer: cli.Localizer,
		AppName:   cli.AppName,
		OnError: func(err error) {
			go showError(cli, "Error during warm-up: %+v", err)
		},
		OnProgressLabel: func(label string) {
			progressLabel.SetText(label)
		},
		OnProgress: func(progress float64) {
			pb.SetValue(int(progress * 1000.0))
		},
		OnSource: func(sourceIn setup.InstallSource) {
			mw.SetTitle(fmt.Sprintf("%s - %s", baseTitle, sourceIn.Version))
			sourceChan <- sourceIn
		},
		OnFinish: func(source setup.InstallSource) {
			// this creates $installDir/app.ico
			err = nwin.CreateUninstallRegistryEntry(cli, installDir, source)
			if err != nil {
				log.Printf("While creating registry entry: %s", err.Error())
			}

			err = nwin.CreateShortcut(nwin.ShortcutSettings{
				ShortcutFilePath: shortcutPath(cli),
				TargetPath:       filepath.Join(installDir, setupName(cli)),
				Description:      "The best way to play your itch.io games",
				IconLocation:     filepath.Join(installDir, "app.ico"),
				WorkingDirectory: filepath.Join(installDir),
			})

			if err != nil {
				showError(cli, "While creating shortcut marker: %+v", err)
				os.Exit(1)
			}

			ni.ShowInfo(cli.AppName, fmt.Sprintf("The installation went well, %s is now starting up!", cli.AppName))

			exePath := filepath.Join(versionInstallDir, exeName(cli))
			cmd := exec.Command(exePath)
			err = cmd.Start()
			if err != nil {
				showError(cli, err.Error(), mw)
			}

			time.Sleep(2 * time.Second)
			os.Exit(0)
		},
	})

	nwin.CenterWindow(mw.AsFormBase())

	mw.Run()
}

func showError(cli cl.CLI, format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	var dlg *walk.Dialog

	log.Printf("Fatal error: %s", msg)

	buf := new(bytes.Buffer)
	fmt.Fprintf(buf, `%s`, cli.Localizer.T("setup.error_dialog.title"))
	buf.WriteString("\n\n")
	fmt.Fprintf(buf, `%s-setup, %s`, cli.AppName, cli.VersionString)
	buf.WriteString("\n\n")
	buf.WriteString(msg)

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
	if mw == nil {
		// go's nil is misused by lxn/walk so we need this
		err = dlgDecl.Create(nil)
	} else {
		err = dlgDecl.Create(mw)
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

func shortcutPath(cli cl.CLI) string {
	return filepath.Join(folders.Desktop, fmt.Sprintf("%s.lnk", cli.AppName))
}

func markerName(cli cl.CLI) string {
	return fmt.Sprintf(".%s-marker", cli.AppName)
}

func exeName(cli cl.CLI) string {
	return fmt.Sprintf("%s.exe", cli.AppName)
}

func setupName(cli cl.CLI) string {
	return fmt.Sprintf("%sSetup.exe", cli.AppName)
}
