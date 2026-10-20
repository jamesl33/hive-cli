package hive

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActions(t *testing.T) {
	slr1 := Receiver{Name: "R", Model: "SLR1c"}
	slr2 := Receiver{Name: "R", Model: "SLR2c"}

	type test struct {
		name   string
		action Action
		want   []string
	}

	tests := []test{
		{
			name:   "Schedule",
			action: HeatSchedule(slr1),
			want:   []string{`{"system_mode":"heat","temperature_setpoint_hold":0,"temperature_setpoint_hold_duration":0}`},
		},
		{
			name:   "Manual",
			action: HeatManual(slr1, 20.5),
			want: []string{
				`{"system_mode":"heat","occupied_heating_setpoint":20.5,"temperature_setpoint_hold":1,"temperature_setpoint_hold_duration":0}`,
				`{"system_mode":"heat","occupied_heating_setpoint":20.5}`,
			},
		},
		{
			name:   "Off",
			action: HeatOff(slr1),
			want:   []string{`{"system_mode":"off","temperature_setpoint_hold":0}`},
		},
		{
			name:   "Boost",
			action: HeatBoost(slr1, 90, 22),
			want:   []string{`{"system_mode":"emergency_heating","temperature_setpoint_hold_duration":90,"temperature_setpoint_hold":1,"occupied_heating_setpoint":22}`},
		},
		{
			name:   "SLR2 off",
			action: HeatOff(slr2),
			want:   []string{`{"system_mode_heat":"off","temperature_setpoint_hold_heat":0}`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got []string

			for _, step := range test.action.Steps {
				b, err := step.Payload.Bytes()
				require.NoError(t, err)

				got = append(got, string(b))
			}

			assert.Equal(t, test.want, got)
		})
	}
}

func TestGet(t *testing.T) {
	b, err := Get([]string{"a", "b"}).Bytes()
	require.NoError(t, err)
	assert.Equal(t, `{"a":"","b":""}`, string(b))
}
