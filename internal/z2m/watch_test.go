package z2m

import (
	"testing"
	"time"

	"github.com/jamesl33/hive-cli/internal/mqtt"
	"github.com/stretchr/testify/assert"
)

func TestParseUpdate(t *testing.T) {
	at := time.Unix(1000, 0)

	names := map[string]bool{"hall": true, "Temperature/Humidity (Office)": true}

	tests := map[string]struct {
		topic   string
		payload string
		device  string
		ok      bool
	}{
		"device":        {"zigbee2mqtt/hall", `{"temperature":19.5}`, "hall", true},
		"slash in name": {"zigbee2mqtt/Temperature/Humidity (Office)", `{"temperature":19.5}`, "Temperature/Humidity (Office)", true},
		"unknown":       {"zigbee2mqtt/garage", `{"temperature":19.5}`, "", false},
		"bridge":        {"zigbee2mqtt/bridge", `{"state":"online"}`, "", false},
		"subtopic":      {"zigbee2mqtt/hall/availability", `{"state":"online"}`, "", false},
		"other prefix":  {"homeassistant/hall", `{"temperature":19.5}`, "", false},
		"not json":      {"zigbee2mqtt/hall", `online`, "", false},
		"not object":    {"zigbee2mqtt/hall", `[1]`, "", false},
	}

	for name, tt := range tests {
		u, ok := parseUpdate(mqtt.Message{Topic: tt.topic, Payload: []byte(tt.payload)}, at, names)
		assert.Equal(t, tt.ok, ok, name)
		assert.Equal(t, tt.device, u.Device, name)
	}
}
