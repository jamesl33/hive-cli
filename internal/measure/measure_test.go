package measure

import (
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/jamesl33/hive-cli/internal/history"
	"github.com/jamesl33/hive-cli/internal/hive"
	"github.com/jamesl33/hive-cli/internal/z2m"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var receiver = hive.Receiver{Name: "hive", Model: "SLR1c"}

func update(t *testing.T, device string, at time.Time, js string) z2m.Update {
	t.Helper()

	payload := make(map[string]any)
	require.NoError(t, json.Unmarshal([]byte(js), &payload))

	return z2m.Update{Device: device, At: at, Payload: payload}
}

func TestExtractSensor(t *testing.T) {
	u := update(t, "hall", time.Unix(1, 0), `{"temperature":19.5,"humidity":48,"battery":87,"linkquality":120}`)

	got := make(map[history.Metric]float64)
	for _, s := range extract(receiver, u, u.Payload) {
		got[s.Metric] = s.Value
	}

	assert.Equal(t, map[history.Metric]float64{history.Temperature: 19.5, history.Humidity: 48, history.Battery: 87}, got)
}

func TestExtractReceiverBoiler(t *testing.T) {
	tests := map[string]float64{
		`{"system_mode":"heat","running_state":"heat"}`: history.BoilerOn,
		`{"system_mode":"heat","running_state":"idle"}`: history.BoilerIdle,
		`{"system_mode":"off","running_state":"idle"}`:  history.BoilerOff,
	}

	for js, want := range tests {
		u := update(t, "hive", time.Unix(1, 0), js)

		samples := extract(receiver, u, u.Payload)
		require.Len(t, samples, 1, js)
		assert.Equal(t, history.Boiler, samples[0].Metric, js)
		assert.Equal(t, want, samples[0].Value, js)
	}
}

func TestReportsValve(t *testing.T) {
	r := NewReports(receiver)

	got := func(js string) map[history.Metric]float64 {
		samples := make(map[history.Metric]float64)
		for _, s := range r.Samples(update(t, "office-trv", time.Unix(1, 0), js)) {
			samples[s.Metric] = s.Value
		}

		return samples
	}

	full := `{"local_temperature":21.5,"occupied_heating_setpoint":16,"heating_valve_position":80,"idle_valve_position":10,"running_state":"idle","battery":90}`

	assert.Equal(t, map[history.Metric]float64{
		history.Temperature: 21.5, history.Setpoint: 16, history.Valve: 10, history.Battery: 90,
		history.ValveHeating: 80, history.ValveIdle: 10, history.ValveState: history.ValveStateIdle,
	}, got(full))

	// The opening follows the running state, using the positions from earlier reports
	assert.Equal(t, map[history.Metric]float64{
		history.Valve: 80, history.ValveState: history.ValveStateHeat,
	}, got(`{"running_state":"heat"}`))
	assert.Empty(t, got(`{"linkquality":40}`))
}

func TestReportsValveOpening(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state map[string]any
		want  float64
	}{
		{"idle uses idle", map[string]any{"running_state": "idle", "idle_valve_position": 30.0, "heating_valve_position": 60.0}, 30},
		{"heat uses heating", map[string]any{"running_state": "heat", "idle_valve_position": 30.0, "heating_valve_position": 60.0}, 60},
		{"closed idle uses heating", map[string]any{"running_state": "idle", "idle_valve_position": 0.0, "heating_valve_position": 60.0}, 60},
		{"closed heating uses idle", map[string]any{"running_state": "heat", "idle_valve_position": 30.0, "heating_valve_position": 0.0}, 30},
		{"both closed", map[string]any{"running_state": "idle", "idle_valve_position": 0.0, "heating_valve_position": 0.0}, 0},
		{"only heating known", map[string]any{"running_state": "idle", "heating_valve_position": 60.0}, 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := valveOpening(tc.state)

			require.True(t, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestReportsSensorHasNoValve(t *testing.T) {
	r := NewReports(receiver)

	for _, s := range r.Samples(update(t, "hall", time.Unix(1, 0), `{"temperature":19,"running_state":"idle"}`)) {
		assert.Equal(t, history.Temperature, s.Metric)
	}
}
