package control

import (
	"testing"

	"github.com/jamesl33/hive-cli/internal/control"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRoom(t *testing.T) {
	for _, tc := range []struct {
		spec string
		want control.Room
	}{
		{"office=tah_office:trv_office", control.Room{Name: "office", Sensor: "tah_office", Valve: "trv_office"}},
		{"office=tah_office:trv_office:-2.5", control.Room{Name: "office", Sensor: "tah_office", Valve: "trv_office", Offset: -2.5}},
		{"landing=tah_landing:trv_landing:1", control.Room{Name: "landing", Sensor: "tah_landing", Valve: "trv_landing", Offset: 1}},
	} {
		t.Run(tc.spec, func(t *testing.T) {
			got, err := parseRoom(tc.spec)
			require.NoError(t, err)

			assert.Equal(t, tc.want, got)
		})
	}
}

func TestOffDefaultsToNone(t *testing.T) {
	cmd := NewControl(nil)

	off, err := cmd.Flags().GetStringArray("off")
	require.NoError(t, err)

	assert.Empty(t, off)
}

func TestTurnOff(t *testing.T) {
	rooms := []control.Room{{Name: "office"}, {Name: "landing"}}

	require.NoError(t, turnOff(rooms, []string{"landing"}))
	assert.False(t, rooms[0].Off)
	assert.True(t, rooms[1].Off)

	assert.Error(t, turnOff(rooms, []string{"lounge"}))

	rooms = []control.Room{{Name: "office"}}

	require.NoError(t, turnOff(rooms, []string{"", "office"}))
	assert.True(t, rooms[0].Off)
}

func TestParseRoomInvalid(t *testing.T) {
	for _, spec := range []string{"", "office", "office=tah_office", "=a:b", "o=:b", "o=a:", "o=a:b:", "o=a:b:warm", "o=a:b:NaN", "o=a:b:Inf", "o=a:b:-Inf", "o=a:b:1:2"} {
		t.Run(spec, func(t *testing.T) {
			_, err := parseRoom(spec)
			assert.Error(t, err)
		})
	}
}

func TestParseRoomsRejectsDuplicates(t *testing.T) {
	c := Control{rooms: []string{"a=s1:v1", "a=s2:v2"}}
	_, err := c.parseRooms()
	assert.Error(t, err)

	c = Control{rooms: []string{"a=s1:v1"}, observe: []string{"b=s2:v1"}}
	_, err = c.parseRooms()
	assert.Error(t, err)

	c = Control{rooms: []string{"a=s1:v1"}, observe: []string{"b=s2:v2"}}
	_, err = c.parseRooms()
	assert.NoError(t, err)
}

func TestRunRejectsBadTick(t *testing.T) {
	c := Control{tick: 0}

	assert.Error(t, c.Run(t.Context()))
}
