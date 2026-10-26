package dashboard

import (
	"testing"
	"time"

	"github.com/jamesl33/hive-cli/internal/history"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummarize(t *testing.T) {
	from := day1.Truncate(24 * time.Hour)
	to := from.Add(4 * time.Hour)
	h := func(n int) time.Time { return from.Add(time.Duration(n) * time.Hour) }

	samples := []history.Sample{
		sample("lounge", history.Temperature, h(-1), 16), // Before the period: the starting value
		sample("radiator", history.Temperature, h(0), 40),
		sample("radiator", history.Valve, h(0), 100),
		sample("hive", history.Temperature, h(0), 17),
		sample("office", history.Temperature, h(0), 18),
		sample("outdoors", history.Temperature, h(0), 8),
		sample("hive", history.Boiler, h(1), history.BoilerOn),
		sample("lounge", history.Temperature, h(2), 17),
		sample("office", history.Temperature, h(2), 17),
		sample("hive", history.Boiler, h(2), history.BoilerIdle),
		sample("hive", history.Boiler, h(3), history.BoilerOn),
	}

	got := Summarize(samples, "outdoors", from, to)

	require.Len(t, got.Groups, 3)
	assert.Equal(t, "Hive", got.Groups[0].Name)
	assert.Equal(t, "Sensors", got.Groups[1].Name)
	assert.Equal(t, "TRVs", got.Groups[2].Name)

	require.Len(t, got.Groups[0].Devices, 1)
	assert.Equal(t, "hive", got.Groups[0].Devices[0].Device)

	sensors := got.Groups[1].Devices
	require.Len(t, sensors, 3)
	assert.Equal(t, "lounge", sensors[0].Device)
	assert.Equal(t, 16.5, sensors[0].Mean)
	assert.Equal(t, 16.0, sensors[0].Min)
	assert.Equal(t, 17.0, sensors[0].Max)
	assert.True(t, sensors[0].Room)
	assert.Equal(t, "office", sensors[1].Device)
	assert.Equal(t, 17.5, sensors[1].Mean)
	assert.Equal(t, "outdoors", sensors[2].Device)
	assert.Equal(t, 8.0, sensors[2].Mean)
	assert.False(t, sensors[2].Room)

	require.Len(t, got.Groups[2].Devices, 1)
	assert.Equal(t, "radiator", got.Groups[2].Devices[0].Device)
	assert.False(t, got.Groups[2].Devices[0].Room)

	assert.InDelta(t, 1.0, got.SpreadMean, 1e-9) // 2h at 2°C, 2h at 0, whatever the radiator reads
	assert.Equal(t, 2.0, got.SpreadMax)
	assert.Equal(t, 2*time.Hour, got.BoilerOn)
	assert.Equal(t, 2, got.BoilerRuns)
}

func TestSummarizeSumsFuelsInThePeriod(t *testing.T) {
	from := day1.Truncate(24 * time.Hour)
	to := from.Add(4 * time.Hour)

	samples := []history.Sample{
		sample("gas", history.Gas, from.Add(-30*time.Minute), 9), // Before the period
		sample("gas", history.Gas, from, 0.5),
		sample("gas", history.Gas, from.Add(30*time.Minute), 0.5), // A repeat is a separate half hour
		sample("electricity", history.Electricity, from.Add(time.Hour), 0.25),
		sample("gas", history.Gas, to.Add(time.Hour), 9), // After the period
	}

	got := Summarize(samples, "outdoors", from, to)

	assert.Equal(t, map[history.Metric]float64{history.Gas: 1, history.Electricity: 0.25}, got.Fuels)
}
