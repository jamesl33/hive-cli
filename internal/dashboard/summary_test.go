package dashboard

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummarize(t *testing.T) {
	from := day1.Truncate(24 * time.Hour)
	to := from.Add(4 * time.Hour)
	h := func(n int) time.Time { return from.Add(time.Duration(n) * time.Hour) }

	samples := []Sample{
		sample("lounge", Temperature, h(-1), 16), // Before the period: the starting value
		sample("office", Temperature, h(0), 18),
		sample("outdoors", Temperature, h(0), 8),
		sample("hive", Boiler, h(1), BoilerOn),
		sample("lounge", Temperature, h(2), 17),
		sample("office", Temperature, h(2), 17),
		sample("hive", Boiler, h(2), BoilerIdle),
		sample("hive", Boiler, h(3), BoilerOn),
	}

	got := Summarize(samples, "outdoors", from, to)

	require.Len(t, got.Rooms, 2)
	assert.Equal(t, "lounge", got.Rooms[0].Device)
	assert.Equal(t, 16.5, got.Rooms[0].Mean)
	assert.Equal(t, 16.0, got.Rooms[0].Min)
	assert.Equal(t, 17.0, got.Rooms[0].Max)
	assert.Equal(t, "office", got.Rooms[1].Device)
	assert.Equal(t, 17.5, got.Rooms[1].Mean)
	require.NotNil(t, got.Outdoors)
	assert.Equal(t, 8.0, got.Outdoors.Mean)
	assert.InDelta(t, 1.0, got.SpreadMean, 1e-9) // 2h at 2°C, 2h at 0
	assert.Equal(t, 2.0, got.SpreadMax)
	assert.Equal(t, 2*time.Hour, got.BoilerOn)
	assert.Equal(t, 2, got.BoilerRuns)
}
