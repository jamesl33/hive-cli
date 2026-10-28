package z2m

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValvePayload(t *testing.T) {
	for percent, want := range map[float64]string{
		55:   `{"system_mode":"off","heating_valve_position":55,"idle_valve_position":55}`,
		-5:   `{"system_mode":"off","heating_valve_position":0,"idle_valve_position":0}`,
		120:  `{"system_mode":"off","heating_valve_position":100,"idle_valve_position":100}`,
		12.6: `{"system_mode":"off","heating_valve_position":13,"idle_valve_position":13}`,
	} {
		b, err := valvePayload(percent)
		require.NoError(t, err)
		assert.JSONEq(t, want, string(b))
	}
}
