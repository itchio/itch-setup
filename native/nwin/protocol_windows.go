package nwin

import (
	"fmt"
	"log"

	"github.com/itchio/itch-setup/cl"
	"golang.org/x/sys/windows/registry"
)

// URLProtocols returns the URL schemes an app handles.
func URLProtocols(appName string) []string {
	if appName == "kitch" {
		return []string{"kitch", "kitchio"}
	}
	return []string{"itch", "itchio"}
}

// RegisterURLProtocols registers the app's URL schemes (per-user) to open
// through the stable launcher rather than a versioned app-X.Y.Z executable,
// which would go stale on the next self-update.
func RegisterURLProtocols(cli cl.CLI, user *UserProfile, launcherPath string) error {
	command := fmt.Sprintf(`"%s" --prefer-launch --appname %s -- "%%1"`, launcherPath, cli.AppName)

	classes, err := user.classesHive()
	if err != nil {
		return err
	}
	defer classes.Close()

	for _, proto := range URLProtocols(cli.AppName) {
		log.Printf("Registering URL protocol (%s:) -> %s for %s", proto, command, user)
		err := registerURLProtocol(classes, proto, launcherPath, command)
		if err != nil {
			return fmt.Errorf("registering %s: protocol: %w", proto, err)
		}
	}
	return nil
}

func registerURLProtocol(classes registry.Key, proto string, launcherPath string, command string) error {
	base := proto

	k, _, err := registry.CreateKey(classes, base, registry.WRITE)
	if err != nil {
		return err
	}
	defer k.Close()

	err = k.SetStringValue("", fmt.Sprintf("URL:%s", proto))
	if err != nil {
		return err
	}
	err = k.SetStringValue("URL Protocol", "")
	if err != nil {
		return err
	}

	ik, _, err := registry.CreateKey(classes, base+`\DefaultIcon`, registry.WRITE)
	if err != nil {
		return err
	}
	defer ik.Close()
	err = ik.SetStringValue("", fmt.Sprintf(`"%s",0`, launcherPath))
	if err != nil {
		return err
	}

	ck, _, err := registry.CreateKey(classes, base+`\shell\open\command`, registry.WRITE)
	if err != nil {
		return err
	}
	defer ck.Close()
	return ck.SetStringValue("", command)
}
