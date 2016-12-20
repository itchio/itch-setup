package main

import (
	"github.com/fasterthanlime/itchSetup/setup"
	"log"
)

func main() {
	var installer *setup.Installer

	done := make(chan bool)

	installer = setup.NewInstaller(setup.InstallerSettings{
		OnError: func(message string) {
			log.Printf("Error: %s\n", message)
			done <- true
		},
		OnFinish: func() {
			log.Printf("All done!")
			done <- true
		},
		OnProgress: func(progress float64) {
			log.Printf("%.2f%% done", progress*100.0)
		},
		OnProgressLabel: func(label string) {
			log.Printf("%s", label)
		},
	})

	installer.Install("/Applications/itch.app")
	<-done
}
