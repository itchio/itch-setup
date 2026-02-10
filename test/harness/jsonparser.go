package harness

import (
	"encoding/json"
	"strings"
)

// MessageType represents the type of JSON message emitted by itch-setup
type MessageType string

const (
	TypeNoUpdateAvailable MessageType = "no-update-available"
	TypeInstallingUpdate  MessageType = "installing-update"
	TypeProgress          MessageType = "progress"
	TypeUpdateReady       MessageType = "update-ready"
	TypeUpdateFailed      MessageType = "update-failed"
	TypeReadyToRelaunch   MessageType = "ready-to-relaunch"
	TypeLog               MessageType = "log"
)

// Message represents a parsed JSON message from itch-setup stdout
type Message struct {
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// NoUpdateAvailablePayload is empty
type NoUpdateAvailablePayload struct{}

// InstallingUpdatePayload contains the version being installed
type InstallingUpdatePayload struct {
	Version string `json:"version"`
}

// ProgressPayload contains download/install progress
type ProgressPayload struct {
	Progress float64 `json:"progress"`
	BPS      float64 `json:"bps"`
	ETA      float64 `json:"eta"`
}

// UpdateReadyPayload contains the version that's ready
type UpdateReadyPayload struct {
	Version string `json:"version"`
}

// UpdateFailedPayload contains the error message
type UpdateFailedPayload struct {
	Message string `json:"message"`
}

// ReadyToRelaunchPayload is empty
type ReadyToRelaunchPayload struct{}

// LogPayload contains log messages
type LogPayload struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// ParseMessage parses a single line of JSON output
func ParseMessage(line string) (Message, bool) {
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "{") {
		return Message{}, false
	}

	var msg Message
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return Message{}, false
	}

	return msg, true
}

// GetInstallingUpdatePayload extracts the payload for installing-update messages
func (m Message) GetInstallingUpdatePayload() (*InstallingUpdatePayload, bool) {
	if m.Type != TypeInstallingUpdate {
		return nil, false
	}
	var p InstallingUpdatePayload
	if err := json.Unmarshal(m.Payload, &p); err != nil {
		return nil, false
	}
	return &p, true
}

// GetProgressPayload extracts the payload for progress messages
func (m Message) GetProgressPayload() (*ProgressPayload, bool) {
	if m.Type != TypeProgress {
		return nil, false
	}
	var p ProgressPayload
	if err := json.Unmarshal(m.Payload, &p); err != nil {
		return nil, false
	}
	return &p, true
}

// GetUpdateReadyPayload extracts the payload for update-ready messages
func (m Message) GetUpdateReadyPayload() (*UpdateReadyPayload, bool) {
	if m.Type != TypeUpdateReady {
		return nil, false
	}
	var p UpdateReadyPayload
	if err := json.Unmarshal(m.Payload, &p); err != nil {
		return nil, false
	}
	return &p, true
}

// GetUpdateFailedPayload extracts the payload for update-failed messages
func (m Message) GetUpdateFailedPayload() (*UpdateFailedPayload, bool) {
	if m.Type != TypeUpdateFailed {
		return nil, false
	}
	var p UpdateFailedPayload
	if err := json.Unmarshal(m.Payload, &p); err != nil {
		return nil, false
	}
	return &p, true
}

// GetLogPayload extracts the payload for log messages
func (m Message) GetLogPayload() (*LogPayload, bool) {
	if m.Type != TypeLog {
		return nil, false
	}
	var p LogPayload
	if err := json.Unmarshal(m.Payload, &p); err != nil {
		return nil, false
	}
	return &p, true
}

// HasMessageType checks if the result contains a message of the given type
func (r *Result) HasMessageType(t MessageType) bool {
	for _, msg := range r.Messages {
		if msg.Type == t {
			return true
		}
	}
	return false
}

// GetFirstMessageOfType returns the first message of the given type
func (r *Result) GetFirstMessageOfType(t MessageType) *Message {
	for _, msg := range r.Messages {
		if msg.Type == t {
			return &msg
		}
	}
	return nil
}

// GetAllMessagesOfType returns all messages of the given type
func (r *Result) GetAllMessagesOfType(t MessageType) []Message {
	var result []Message
	for _, msg := range r.Messages {
		if msg.Type == t {
			result = append(result, msg)
		}
	}
	return result
}
