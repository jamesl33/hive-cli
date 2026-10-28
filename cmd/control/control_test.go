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

func TestParseRoomInvalid(t *testing.T) {
	for _, spec := range []string{"", "office", "office=tah_office", "=a:b", "o=:b", "o=a:", "o=a:b:", "o=a:b:warm", "o=a:b:NaN", "o=a:b:Inf", "o=a:b:-Inf", "o=a:b:1:2"} {
		t.Run(spec, func(t *testing.T) {
			_, err := parseRoom(spec)
			assert.Error(t, err)
		})
	}
}
