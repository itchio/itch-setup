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

	fmt.Fprintf(os.Stderr, "%s\n", string(bs))
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

type UpdateFailed struct {
	Message string `json:"message"`
}

func (p UpdateFailed) GetType() string { return "update-failed" }
