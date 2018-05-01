package native

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/itchio/itch-setup/cl"
	"github.com/itchio/itch-setup/setup"
	"github.com/lxn/walk"
	ui "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	ps "github.com/mitchellh/go-ps"
)

func getUserDirectory(csidl win.CSIDL) (string, error) {
	localPathPtr := make([]uint16, 65536+2)
	var hwnd win.HWND
	success := win.SHGetSpecialFolderPath(hwnd, &localPathPtr[0], csidl, true)
	if !success {
		return "", errors.New("Could not get folder path")
	}
	return syscall.UTF16ToString(localPathPtr), nil
}

var localPath, roamingPath, desktopPath string

func Do(cli cl.CLI) {
	var err error

	localPath, err = getUserDirectory(win.CSIDL_LOCAL_APPDATA)
	if err != nil {
		showError(cli, err.Error(), nil)
		os.Exit(1)
	}

	roamingPath, err = getUserDirectory(win.CSIDL_APPDATA)
	if err != nil {
		showError(cli, err.Error(), nil)
		os.Exit(1)
	}

	desktopPath, err = getUserDirectory(win.CSIDL_DESKTOP)
	if err != nil {
		showError(cli, err.Error(), nil)
		os.Exit(1)
	}

	log.Println("AppData local path: ", localPath)
	log.Println("AppData roam' path: ", roamingPath)
	log.Println("Desktop path:       ", desktopPath)

	foundMarker, execFolder, appDirs, err := pokeExecFolder(cli)

	if foundMarker {
		log.Println("Found marker")

		if cli.Uninstall {
			log.Println("Uninstalling app")

			pathsToKill := []string{}
			for _, appDir := range appDirs {
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
			for _, appDir := range appDirs {
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
			return
		}

		if cli.Relaunch {
			proc, err := os.FindProcess(cli.RelaunchPID)
			if err != nil {
				showError(cli, fmt.Sprintf("Could not find %s app process: %s", cli.AppName, err.Error()), nil)
			}

			state, err := proc.Wait()
			if err != nil {
				showError(cli, fmt.Sprintf("Could not wait on %s app: %s", cli.AppName, err.Error()), nil)
			}

			log.Printf("Wait result: success = %v", state.Success())

			tryLaunch(cli, appDirs)
			return
		}

		{
			tryLaunch(cli, appDirs)
			return
		}
	}

	if cli.Uninstall {
		log.Printf("Asked to uninstall but couldn't find marker, just quitting")
		os.Exit(0)
	}

	log.Println("Showing install GUI")
	installDir := filepath.Join(localPath, cli.AppName)
	showInstallGUI(cli, installDir)
}

// TODO: return a struct damn it
func pokeExecFolder(cli cl.CLI) (foundMarker bool, execFolder string, appDirs []string, err error) {
	execPath, err := os.Executable()
	if err != nil {
		return
	}

	execFolder = filepath.Dir(execPath)

	var entries []os.FileInfo

	entries, err = ioutil.ReadDir(execFolder)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			if entry.Name() == markerName(cli) {
				foundMarker = true
			}
			continue
		}

		if !strings.HasPrefix(entry.Name(), "app-") {
			continue
		}

		appDirs = append(appDirs, entry.Name())
	}
	sort.Strings(appDirs)

	// make all paths absolute
	for i := range appDirs {
		appDirs[i] = filepath.Join(execFolder, appDirs[i])
	}

	return
}

func tryLaunch(cli cl.CLI, appDirs []string) {
	log.Println("Launching app")

	log.Printf("Sorted app dirs:\n%s", strings.Join(appDirs, "\n"))

	if len(appDirs) > 0 {
		first := appDirs[0]
		cmd := exec.Command(filepath.Join(first, exeName(cli)))

		err := cmd.Start()
		if err != nil {
			showError(cli, fmt.Sprintf("Encountered a problem while launching %s: %s", cli.AppName, err.Error()), nil)
		}

		log.Printf("App launched, getting out of the way")
		os.Exit(0)
	}
}

func showInstallGUI(cli cl.CLI, installDirIn string) {
	var installer *setup.Installer

	var ni *walk.NotifyIcon
	var installDirLabel *walk.LineEdit
	var pb *walk.ProgressBar
	var progressLabel *walk.Label
	var mw *walk.MainWindow
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
			showError(cli, kickErr.Error(), mw)
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
			go showError(cli, fmt.Sprintf("%v", err), mw)
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
				log.Println("While creating marker", err)
				showError(cli, err.Error(), mw)
				os.Exit(1)
			}
			err = markerWriter.Close()
			if err != nil {
				log.Println("While closing marker", err)
				showError(cli, err.Error(), mw)
				os.Exit(1)
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
				log.Println("While creating shortcut", err)
				showError(cli, err.Error(), mw)
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

func showError(cli cl.CLI, errMsg string, parent walk.Form) {
	var dlg *walk.Dialog

	res, err := ui.Dialog{
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
						Text: errMsg,
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
	}.Run(parent)

	centerWindow(dlg.AsFormBase())

	if err != nil {
		log.Printf("Error in dialog: %s\n", err.Error())
	}
	log.Printf("Dialog res: %#v\n", res)

	os.Exit(0)
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
