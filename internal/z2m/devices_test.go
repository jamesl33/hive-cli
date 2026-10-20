package z2m

import (
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func devices(t *testing.T, in string) []Device {
	t.Helper()

	var devices []Device

	require.NoError(t, json.Unmarshal([]byte(in), &devices))

	return devices
}

func TestPickReceiver(t *testing.T) {
	r, err := pickReceiver(devices(t, `[
		{"friendly_name":"Coordinator","definition":null},
		{"friendly_name":"Thermostat","definition":{"model":"SLT3C","vendor":"Hive"}},
		{"friendly_name":"Boiler","definition":{"model":"SLR1c","vendor":"Hive"}}
	]`))
	require.NoError(t, err)
	assert.Equal(t, "Boiler", r.Name)
}

func TestPickReceiverNone(t *testing.T) {
	_, err := pickReceiver(devices(t, `[{"friendly_name":"Coordinator","definition":null}]`))
	assert.Error(t, err)
}

func TestPickReceiverSeveral(t *testing.T) {
	_, err := pickReceiver(devices(t, `[
		{"friendly_name":"A","definition":{"model":"SLR1c","vendor":"Hive"}},
		{"friendly_name":"B","definition":{"model":"SLR2c","vendor":"Hive"}}
	]`))
	assert.Error(t, err)
}
