package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"time"

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

// CreateUninstallRegistryEntry creates all registry entries required to
// have the app show up in Add or Remove software and be uninstalled by the user
func CreateUninstallRegistryEntry(installDir string, appNameIn string, version string) error {
	appName := appNameIn + "-experimental"

	pk, _, err := registry.CreateKey(registry.CURRENT_USER, "Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall", registry.CREATE_SUB_KEY)
	if err != nil {
		return err
	}
	defer pk.Close()

	k, _, err := registry.CreateKey(pk, "itch-experimental", registry.CREATE_SUB_KEY|registry.WRITE)
	if err != nil {
		return err
	}
	defer k.Close()

	appData := os.Getenv("APPDATA")
	uninstallCmd := fmt.Sprintf("\"%s\" --uninstall", filepath.Join(appData, appName, "bin", "itchSetup.exe"))

	strings := []StringValue{
		{Key: "DisplayName", Value: appName},
		{Key: "DisplayVersion", Value: version},
		{Key: "InstallDate", Value: time.Now().Format(RegDateFormat)},
		{Key: "InstallLocation", Value: installDir},
		{Key: "Publisher", Value: "Itch Corp"},
		{Key: "QuietUninstallString", Value: uninstallCmd},
		{Key: "UninstallString", Value: uninstallCmd},
		{Key: "URLUpdateInfo", Value: "https://itch.io/app"},
	}

	func() {
		iconPath := filepath.Join(appData, fmt.Sprintf("%s.ico", appName))
		icoBytes, err := dataItchIcoBytes()
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
		{Key: "EstimatedSize", Value: uint32(float64(sizeof(installDir) / 1024.0))},
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

func sizeof(path string) int64 {
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
