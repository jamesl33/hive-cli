package z2m

import (
	"testing"

	"github.com/jamesl33/hive-cli/internal/hive"
	"github.com/jamesl33/hive-cli/internal/mqtt"
	"github.com/stretchr/testify/assert"
)

func TestAbsorb(t *testing.T) {
	s := &Session{Receiver: hive.Receiver{Name: "R"}, State: make(map[string]any), topic: "zigbee2mqtt/R"}

	_, ok := s.absorb(mqtt.Message{Topic: "zigbee2mqtt/R", Payload: []byte(`{"system_mode":"off"}`), Retained: true})
	assert.False(t, ok)
	assert.Empty(t, s.State)

	_, ok = s.absorb(mqtt.Message{Topic: "zigbee2mqtt/other", Payload: []byte(`{"system_mode":"off"}`)})
	assert.False(t, ok)

	props, ok := s.absorb(mqtt.Message{Topic: "zigbee2mqtt/R", Payload: []byte(`{"system_mode":"heat"}`)})
	assert.True(t, ok)
	assert.Equal(t, []string{"system_mode"}, props)
	assert.Equal(t, "heat", s.State["system_mode"])
}

func TestReadingComplete(t *testing.T) {
	r := reading{wanted: []string{"a", "b"}, seen: make(map[string]bool)}

	r.record([]string{"a"})
	assert.False(t, r.complete)

	r.record([]string{"b"})
	assert.True(t, r.complete)
}
