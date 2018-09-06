package nwin

import (
	"os"
	"path/filepath"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"

	"github.com/scjalliance/comshim"
)

type ShortcutSettings struct {
	ShortcutFilePath string
	TargetPath       string
	Arguments        string
	Description      string
	IconLocation     string
	WorkingDirectory string
}

// CreateShortcut creates a windows shortcut with the given settings
func CreateShortcut(settings ShortcutSettings) error {
	dir := filepath.Dir(settings.ShortcutFilePath)
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	comshim.Add(1)
	defer comshim.Done()

	oleShellObject, err := oleutil.CreateObject("WScript.Shell")
	if err != nil {
		return err
	}
	defer oleShellObject.Release()

	wshell, err := oleShellObject.QueryInterface(ole.IID_IDispatch)
	if err != nil {
		return err
	}
	defer wshell.Release()

	cs, err := oleutil.CallMethod(wshell, "CreateShortcut", settings.ShortcutFilePath)
	if err != nil {
		return err
	}
	idispatch := cs.ToIDispatch()
	oleutil.PutProperty(idispatch, "TargetPath", settings.TargetPath)
	oleutil.PutProperty(idispatch, "Arguments", settings.Arguments)
	oleutil.PutProperty(idispatch, "Description", settings.Description)
	oleutil.PutProperty(idispatch, "IconLocation", settings.IconLocation)
	oleutil.PutProperty(idispatch, "WorkingDirectory", settings.WorkingDirectory)
	_, err = oleutil.CallMethod(idispatch, "Save")
	if err != nil {
		return err
	}

	return nil
}
