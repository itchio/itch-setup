package nwin

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
)

type ShortcutSettings struct {
	ShortcutFilePath string
	TargetPath       string
	Arguments        string
	Description      string
	IconLocation     string
	WorkingDirectory string
}

const windowsShortcutContent = `
	set WshShell = WScript.CreateObject("WScript.Shell")
	set shellLink = WshShell.CreateShortcut(%q)
	shellLink.TargetPath = %q
	shellLink.Arguments = %q
	shellLink.Description = %q
	shellLink.IconLocation = %q
	shellLink.WorkingDirectory = %q
	shellLink.Save`

func CreateShortcut(settings ShortcutSettings) error {
	shortcutScript := fmt.Sprintf(windowsShortcutContent,
		settings.ShortcutFilePath,
		settings.TargetPath,
		settings.Arguments,
		settings.Description,
		settings.IconLocation,
		settings.WorkingDirectory)

	tmpDir, err := ioutil.TempDir("", "itch-setup-shortcut")
	if err != nil {
		return err
	}

	err = os.MkdirAll(tmpDir, 0755)
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)

	tmpPath := filepath.Join(tmpDir, "makeShortcut.vbs")
	err = ioutil.WriteFile(tmpPath, []byte(shortcutScript), 0644)
	if err != nil {
		return err
	}

	cmd := exec.Command("wscript", "/b", "/nologo", tmpPath)
	return cmd.Run()
}
