package nwin

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/jxeng/shortcut"
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
		return fmt.Errorf("shortcut file path is not absolute: %q", settings.ShortcutFilePath)
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

	sc := shortcut.Shortcut{
		ShortcutPath:     settings.ShortcutFilePath,
		Target:           settings.TargetPath,
		Arguments:        settings.Arguments,
		Description:      settings.Description,
		IconLocation:     settings.IconLocation,
		WorkingDirectory: settings.WorkingDirectory,
	}

	return shortcut.Create(sc)
}
