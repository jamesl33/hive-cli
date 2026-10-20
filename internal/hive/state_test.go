package hive

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func state(t *testing.T, js string) map[string]any {
	t.Helper()

	m := make(map[string]any)
	require.NoError(t, json.Unmarshal([]byte(js), &m))

	return m
}

func TestInterpret(t *testing.T) {
	r := Receiver{Model: "SLR1c"}

	tests := map[string]string{
		`{"system_mode":"heat","temperature_setpoint_hold":false}`:                                                     ModeSchedule,
		`{"system_mode":"heat","temperature_setpoint_hold":true}`:                                                      ModeManual,
		`{"system_mode":"off","temperature_setpoint_hold":false}`:                                                      ModeOff,
		`{"system_mode":"emergency_heating","temperature_setpoint_hold":true,"temperature_setpoint_hold_duration":20}`: ModeBoost,
		`{}`: ModeUnknown,
	}

	for js, want := range tests {
		assert.Equal(t, want, Interpret(r, state(t, js)).Mode, js)
	}
}

func TestInterpretHidesGarbage(t *testing.T) {
	js := `{"system_mode":"emergency_heating","temperature_setpoint_hold_duration":65535,"occupied_heating_setpoint":1}`

	cs := Interpret(Receiver{Model: "SLR1c"}, state(t, js))
	assert.Nil(t, cs.BoostLeft)
	assert.Nil(t, cs.Target)
}

func TestStatusKeys(t *testing.T) {
	assert.Len(t, StatusKeys(Receiver{Model: "SLR1c"}), 6)
	assert.Len(t, StatusKeys(Receiver{Model: "SLR2c"}), 6)
}

func TestStateString(t *testing.T) {
	target, room, active, left := 21.0, 19.4, true, 20

	cs := State{Mode: ModeBoost, Target: &target, Temperature: &room, Active: &active, BoostLeft: &left}

	assert.Equal(t, "Heating: boost, target 21°C, 20 min left, room 19.4°C, boiler on", cs.String())
}
