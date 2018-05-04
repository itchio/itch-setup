package nwin

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/itchio/itch-setup/setup"

	"github.com/itchio/itch-setup/bindata"
	"github.com/itchio/itch-setup/cl"
	"golang.org/x/sys/windows/registry"
)

// RegDateFormat is the format in which installed time should be stored in the registry,
// as a format string suitable for time.Format()
const RegDateFormat = "20060102"

type StringValue struct {
	Key   string
	Value string
}

type DWORDValue struct {
	Key   string
	Value uint32
}

const uninstallRegPrefix = "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall"

func GetRegistryInstallDir(cli cl.CLI) (string, error) {
	pk, err := registry.OpenKey(registry.CURRENT_USER, uninstallRegPrefix, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return "", err
	}
	defer pk.Close()

	k, err := registry.OpenKey(pk, cli.AppName, registry.READ)
	if err != nil {
		return "", err
	}
	defer k.Close()

	installDir, _, err := k.GetStringValue("InstallLocation")
	if err != nil {
		return "", err
	}

	return installDir, nil
}

// CreateUninstallRegistryEntry creates all registry entries required to
// have the app show up in Add or Remove software and be uninstalled by the user
func CreateUninstallRegistryEntry(cli cl.CLI, installDir string, source setup.InstallSource) error {
	pk, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallRegPrefix, registry.CREATE_SUB_KEY)
	if err != nil {
		return err
	}
	defer pk.Close()

	k, _, err := registry.CreateKey(pk, cli.AppName, registry.WRITE)
	if err != nil {
		return err
	}
	defer k.Close()

	uninstallCmd := fmt.Sprintf("\"%s\" --uninstall", filepath.Join(installDir, "itch-setup.exe"))

	strings := []StringValue{
		{Key: "DisplayName", Value: cli.AppName},
		{Key: "DisplayVersion", Value: source.Version},
		{Key: "InstallDate", Value: time.Now().Format(RegDateFormat)},
		{Key: "InstallLocation", Value: installDir},
		{Key: "Publisher", Value: "itch corp."},
		{Key: "QuietUninstallString", Value: uninstallCmd},
		{Key: "UninstallString", Value: uninstallCmd},
		{Key: "URLUpdateInfo", Value: "https://itch.io/app"},
	}

	func() {
		iconPath := filepath.Join(installDir, "app.ico")
		icoBytes, err := bindata.Asset("data/itch.ico")
		if err != nil {
			log.Printf("itch ico not found :()")
			return
		}

		err = ioutil.WriteFile(iconPath, icoBytes, os.FileMode(0644))
		if err != nil {
			log.Printf("could not write itch ico")
			return
		}

		strings = append(strings, StringValue{
			Key:   "DisplayIcon",
			Value: iconPath,
		})
	}()

	dwords := []DWORDValue{
		{Key: "EstimatedSize", Value: uint32(float64(folderSize(installDir) / 1024.0))},
		{Key: "NoModify", Value: 1},
		{Key: "NoRepair", Value: 1},
		{Key: "Language", Value: 0x0409},
	}

	for _, sv := range strings {
		err = k.SetStringValue(sv.Key, sv.Value)
		if err != nil {
			return err
		}
	}

	for _, dv := range dwords {
		err = k.SetDWordValue(dv.Key, dv.Value)
		if err != nil {
			return err
		}
	}

	return nil
}

func RemoveUninstallerRegistryKey(cli cl.CLI) error {
	pk, _, err := registry.CreateKey(registry.CURRENT_USER, uninstallRegPrefix, registry.WRITE)
	if err != nil {
		return err
	}
	defer pk.Close()

	err = registry.DeleteKey(pk, cli.AppName)
	if err != nil {
		return err
	}
	return nil
}

func folderSize(path string) int64 {
	totalSize := int64(0)

	inc := func(_ string, f os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		totalSize += f.Size()
		return nil
	}

	filepath.Walk(path, inc)
	return totalSize
}
