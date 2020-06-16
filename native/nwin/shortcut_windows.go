package nwin

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/itchio/husk/husk"
)

type ShortcutSettings struct {
	ShortcutFilePath string
	TargetPath       string
	Arguments        string
	Description      string
	IconLocation     string
	WorkingDirectory string
	OnlyIfExists     bool
}

// CreateShortcut creates a windows shortcut with the given settings
func CreateShortcut(settings ShortcutSettings) error {
	if !filepath.IsAbs(settings.ShortcutFilePath) {
		return fmt.Errorf("Shortcut file path is not absolute: %q", settings.ShortcutFilePath)
	}

	if settings.OnlyIfExists {
		_, err := os.Stat(settings.ShortcutFilePath)
		if err != nil {
			log.Printf("Not updating shortcut (%s): %v", settings.ShortcutFilePath, err)
			return nil
		}
	}

	dir := filepath.Dir(settings.ShortcutFilePath)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	// TODO: arguments, description, workingdirectory, iconlocation,

	sl, err := husk.NewShellLink()
	if err != nil {
		return err
	}

	err = sl.SetPath(settings.TargetPath)
	if err != nil {
		return err
	}

	err = sl.SetArguments(settings.Arguments)
	if err != nil {
		return err
	}

	err = sl.SetDescription(settings.Description)
	if err != nil {
		return err
	}

	err = sl.SetWorkingDirectory(settings.WorkingDirectory)
	if err != nil {
		return err
	}

	err = sl.SetIconLocation(settings.IconLocation, 0)
	if err != nil {
		return err
	}

	err = sl.Save(settings.ShortcutFilePath)
	if err != nil {
		return err
	}

	return nil
}
