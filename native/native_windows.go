package native

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/pkg/errors"

	"github.com/itchio/itch-setup/cl"
	"github.com/itchio/itch-setup/setup"
	"github.com/lxn/walk"
	ui "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	ps "github.com/mitchellh/go-ps"
)

var mw *walk.MainWindow = nil

func getUserDirectory(csidl win.CSIDL) (string, error) {
	localPathPtr := make([]uint16, 65536+2)
	var hwnd win.HWND
	success := win.SHGetSpecialFolderPath(hwnd, &localPathPtr[0], csidl, true)
	if !success {
		return "", errors.New("Could not get folder path")
	}
	return syscall.UTF16ToString(localPathPtr), nil
}

var localPath, roamingPath, desktopPath, execFolder string

func getDirs() error {
	var err error

	localPath, err = getUserDirectory(win.CSIDL_LOCAL_APPDATA)
	if err != nil {
		return err
	}

	roamingPath, err = getUserDirectory(win.CSIDL_APPDATA)
	if err != nil {
		return err
	}

	desktopPath, err = getUserDirectory(win.CSIDL_DESKTOP)
	if err != nil {
		return err
	}

	execPath, err := os.Executable()
	if err != nil {
		return err
	}

	execFolder = filepath.Dir(execPath)

	log.Println("AppData local path: ", localPath)
	log.Println("AppData roam' path: ", roamingPath)
	log.Println("Desktop path:       ", desktopPath)
	return nil
}

func Do(cli cl.CLI) {
	var err error

	err = getDirs()
	if err != nil {
		showError(cli, "Error during setup initialization: %+v", err)
	}

	baseDir := filepath.Join(localPath, cli.AppName)
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
	installDir := filepath.Join(localPath, cli.AppName)
	showInstallGUI(cli, installDir)
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

	pidsToKill := []int{}

	processes, err := ps.Processes()
	if err != nil {
		log.Println("While getting process list", err.Error())
		log.Println("(Note: this just means we won't be able to kill running instances)")
	} else {
		for _, process := range processes {
			func() {
				handle, err := syscall.OpenProcess(syscall.PROCESS_QUERY_INFORMATION, false, uint32(process.Pid()))
				if err != nil {
					log.Printf("Couldn't open process (pid %d): %s", process.Pid(), err.Error())
				} else {
					defer syscall.Close(handle)
					fullName, err := GetModuleFileName(handle)
					if err != nil {
						log.Printf("Couldn't get module file name (pid %d): %s", process.Pid(), err.Error())
					} else {
						for _, pathToKill := range pathsToKill {
							if fullName == pathToKill {
								log.Printf("Should kill %d", process.Pid())
								pidsToKill = append(pidsToKill, process.Pid())
							}
						}
					}
				}
			}()
		}

		log.Printf("%d processes to kill", len(pidsToKill))
		for _, pidToKill := range pidsToKill {
			func() {
				p, err := os.FindProcess(pidToKill)
				if err != nil {
					// oh well
					log.Printf("PID %d vanished", pidToKill)
					return
				}

				log.Printf("Killing %d...", pidToKill)

				// not even going to bother with the error code - if it works, great! if it doesn't, oh well
				p.Kill()
			}()
		}
	}

	log.Println("Removing marker")
	err = os.Remove(filepath.Join(execFolder, markerName(cli)))
	if err != nil {
		log.Println("While removing marker", err.Error())
	}

	log.Println("Removing icon")
	err = os.Remove(filepath.Join(execFolder, "app.ico"))
	if err != nil {
		log.Println("While removing icon", err.Error())
	}

	log.Println("Removing shortcut...")
	err = os.Remove(shortcutPath(cli))
	if err != nil {
		log.Println("While removing full shortcut", err.Error())
		log.Println("(Note: shortcut errors aren't critical)")
	}

	log.Println("Removing all versions...")
	for _, appDir := range mv.ListAppDirs() {
		tries := 5
		for i := 0; i < tries; i++ {
			err = os.RemoveAll(appDir)
			if err != nil {
				log.Println("While removing", filepath.Base(appDir), err.Error())
				log.Println("Sleeping a bit then retrying")
				time.Sleep(1 * time.Second)
				continue
			}
			break
		}
	}

	log.Println("Removing uninstaller entry...")
	err = RemoveUninstallerRegistryKey(cli.AppName)
	if err != nil {
		log.Println("While removing uninstaller entry", err.Error())
		log.Println("(Note: these aren't critical)")
	}
	return nil
}

func doRelaunch(cli cl.CLI, mv setup.Multiverse) error {
	proc, err := os.FindProcess(cli.RelaunchPID)
	if err != nil {
		return errors.WithMessage(err, "could not find app process to wait on")
	}

	state, err := proc.Wait()
	if err != nil {
		return errors.WithMessage(err, "could not wait on app before relaunch")
	}

	log.Printf("Wait result: success = %v", state.Success())

	if appDir, ok := mv.GetValidAppDir(); ok {
		log.Printf("Found valid app dir, relaunching")
		tryLaunch(cli, appDir)
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

	var source setup.InstallSource
	sourceChan := make(chan setup.InstallSource, 1)

	kickoffInstall := func() {
		kickErr := func() error {
			err := os.MkdirAll(installDir, 0755)
			if err != nil {
				return err
			}

			execPath, err := os.Executable()
			if err != nil {
				return err
			}

			// copy ourselves in install directory
			copyErr := func() error {
				installedExecPath := filepath.Join(installDir, setupName(cli))
				execWriter, err := os.Create(installedExecPath)
				if err != nil {
					log.Println("Couldn't open target for writing, maybe already running from install location?")
					log.Println("Not copying and carrying on...")
					return nil
				}
				defer execWriter.Close()

				execReader, err := os.OpenFile(execPath, os.O_RDONLY, 0)
				if err != nil {
					return err
				}
				defer execReader.Close()

				_, err = io.Copy(execWriter, execReader)
				return err
			}()
			if copyErr != nil {
				return copyErr
			}

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

	setInstallerImage(imageView)

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
			source = sourceIn
		},
		OnFinish: func(source setup.InstallSource) {
			markerPath := filepath.Join(installDir, markerName(cli))
			markerWriter, err := os.Create(markerPath)
			if err != nil {
				showError(cli, "While creating marker: %+v", err)
			}
			err = markerWriter.Close()
			if err != nil {
				showError(cli, "While closing marker: %+v", err)
			}

			// this creates $installDir/app.ico
			err = CreateUninstallRegistryEntry(installDir, cli.AppName, source.Version)
			if err != nil {
				log.Printf("While creating registry entry: %s", err.Error())
			}

			err = CreateShortcut(ShortcutSettings{
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

	centerWindow(mw.AsFormBase())

	mw.Run()
}

func showError(cli cl.CLI, format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	var dlg *walk.Dialog

	log.Printf("Fatal error: %s", msg)

	var err error
	dlgDecl := ui.Dialog{
		Title:    cli.Localizer.T("setup.error_dialog.title"),
		MinSize:  ui.Size{Width: 350},
		Layout:   ui.VBox{},
		AssignTo: &dlg,
		Children: []ui.Widget{
			ui.Composite{
				Layout: ui.HBox{
					MarginsZero: true,
				},
				Children: []ui.Widget{
					ui.Label{
						Text: msg,
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

	var res int
	if mw == nil {
		// go's nil is misused by lxn/walk so we need this
		res, err = dlgDecl.Run(nil)
	} else {
		res, err = dlgDecl.Run(mw)
	}

	centerWindow(dlg.AsFormBase())

	if err != nil {
		log.Printf("Error in dialog: %s\n", err.Error())
	}
	log.Printf("Dialog res: %#v\n", res)

	os.Exit(1)
}

func shortcutPath(cli cl.CLI) string {
	return filepath.Join(desktopPath, fmt.Sprintf("%s.lnk", cli.AppName))
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
