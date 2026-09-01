package setup

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
)

type Payload interface {
	GetType() string
}

type message struct {
	Type    string  `json:"type"`
	Payload Payload `json:"payload"`
}

var jsonEnabled = false
var jsonLock sync.Mutex
var jsonLogFile *os.File

// SetLogFile mirrors every emitted message to path (appending), for
// callers that can't read our stdout, such as the app after we've
// re-executed ourselves elevated.
func SetLogFile(path string) error {
	jsonLock.Lock()
	defer jsonLock.Unlock()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	if jsonLogFile != nil {
		jsonLogFile.Close()
	}
	jsonLogFile = f
	return nil
}

func EnableJSON() {
	jsonLock.Lock()
	defer jsonLock.Unlock()

	jsonEnabled = true
}

func DisableJSON() {
	jsonLock.Lock()
	defer jsonLock.Unlock()

	jsonEnabled = false
}

func Emit(p Payload) {
	jsonLock.Lock()
	defer jsonLock.Unlock()

	if !jsonEnabled {
		return
	}

	m := &message{
		Type:    p.GetType(),
		Payload: p,
	}

	bs, err := json.Marshal(m)
	if err != nil {
		log.Printf("Could not send JSON object: %+v", err)
		return
	}

	fmt.Fprintf(os.Stdout, "%s\n", string(bs))
	if jsonLogFile != nil {
		fmt.Fprintf(jsonLogFile, "%s\n", string(bs))
		jsonLogFile.Sync()
	}
}

//-------------------------------

type Log struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

func (p Log) GetType() string { return "log" }

//-------------------------------

type Progress struct {
	Progress float64 `json:"progress"`
	BPS      float64 `json:"bps"`
	ETA      float64 `json:"eta"`
}

func (p Progress) GetType() string { return "progress" }

//-------------------------------

type InstallingUpdate struct {
	Version string `json:"version"`
}

func (p InstallingUpdate) GetType() string { return "installing-update" }

//-------------------------------

type UpdateReady struct {
	Version string `json:"version"`
}

func (p UpdateReady) GetType() string { return "update-ready" }

//-------------------------------

type NoUpdateAvailable struct{}

func (p NoUpdateAvailable) GetType() string { return "no-update-available" }

//-------------------------------

// An update is available but the install folder isn't writable by the
// current user; applying it needs an elevated run (--elevate).
type UpdateRequiresElevation struct {
	Version string `json:"version"`
}

func (p UpdateRequiresElevation) GetType() string { return "update-requires-elevation" }

//-------------------------------

type UpdateFailed struct {
	Message string `json:"message"`
}

func (p UpdateFailed) GetType() string { return "update-failed" }

//-------------------------------

type ReadyToRelaunch struct{}

func (p ReadyToRelaunch) GetType() string { return "ready-to-relaunch" }
