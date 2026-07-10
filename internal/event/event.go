package event

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	SchemaVersion = 1
	MaxEventBytes = 16 * 1024
	MaxBatchBytes = 1024 * 1024
)

type Event struct {
	SchemaVersion   int             `json:"schemaVersion"`
	EventID         string          `json:"eventId"`
	EventType       string          `json:"eventType"`
	OccurredAt      time.Time       `json:"occurredAt"`
	MonotonicTimeNS *int64          `json:"monotonicTimeNs,omitempty"`
	InstanceID      string          `json:"instanceId"`
	ProducerID      string          `json:"producerId"`
	ProcessID       *int            `json:"processId,omitempty"`
	Sequence        uint64          `json:"sequence"`
	Source          string          `json:"source"`
	Payload         json.RawMessage `json:"payload"`
}

var allowedTypes = map[string]struct{}{
	"gateway.started": {}, "gateway.heartbeat": {}, "gateway.stopped": {}, "gateway.crashed": {},
	"session.started": {}, "session.completed": {}, "session.failed": {},
	"agent.started": {}, "agent.completed": {}, "agent.failed": {},
	"llm.started": {}, "llm.completed": {}, "llm.failed": {}, "llm.retried": {},
	"tool.started": {}, "tool.completed": {}, "tool.failed": {},
	"mcp.started": {}, "mcp.completed": {}, "mcp.failed": {},
	"subagent.started": {}, "subagent.completed": {}, "subagent.failed": {},
	"resource.sampled": {}, "monitor.events_dropped": {}, "monitor.plugin_error": {},
}

func (e Event) Validate(now time.Time) error {
	if e.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schemaVersion %d", e.SchemaVersion)
	}
	if len(e.EventID) < 32 || len(e.EventID) > 64 {
		return errors.New("invalid eventId")
	}
	if _, ok := allowedTypes[e.EventType]; !ok {
		return fmt.Errorf("unsupported eventType %q", e.EventType)
	}
	if e.OccurredAt.IsZero() || e.OccurredAt.Before(time.Unix(0, 0)) || e.OccurredAt.After(now.Add(24*time.Hour)) {
		return errors.New("invalid occurredAt")
	}
	if !validID(e.InstanceID, 128) || !validID(e.ProducerID, 128) {
		return errors.New("invalid instanceId or producerId")
	}
	if e.Source != "plugin" && e.Source != "daemon" {
		return errors.New("invalid source")
	}
	if e.ProcessID != nil && (*e.ProcessID < 1 || *e.ProcessID > 1<<31-1) {
		return errors.New("invalid processId")
	}
	if len(e.Payload) == 0 || e.Payload[0] != '{' || !json.Valid(e.Payload) {
		return errors.New("payload must be a JSON object")
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if len(b) > MaxEventBytes {
		return fmt.Errorf("event exceeds %d bytes", MaxEventBytes)
	}
	return nil
}

func validID(s string, max int) bool {
	if len(s) == 0 || len(s) > max {
		return false
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("._:-", r) {
			continue
		}
		return false
	}
	return true
}

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	buf := make([]byte, 36)
	hex.Encode(buf[0:8], b[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], b[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], b[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], b[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], b[10:16])
	return string(buf)
}

func PayloadMap(raw json.RawMessage) map[string]any {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}

func String(m map[string]any, key string) string {
	v, _ := m[key].(string)
	if len(v) > 512 {
		return v[:512]
	}
	return v
}

func Float(m map[string]any, key string) float64 {
	v, _ := m[key].(float64)
	return v
}

func Bool(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}
