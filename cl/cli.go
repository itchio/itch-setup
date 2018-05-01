package cl

import "github.com/itchio/itch-setup/localize"

// globals, get your globals here!

type CLI struct {
	AppName       string
	VersionString string

	PreferLaunch bool
	Localizer    *localize.Localizer
}
