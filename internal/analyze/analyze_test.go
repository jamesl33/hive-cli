package analyze

import (
	"testing"
	"time"

	"github.com/jamesl33/hive-cli/internal/history"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var day1 = time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)

func sample(device string, metric history.Metric, at time.Time, v float64) history.Sample {
	return history.Sample{Device: device, Metric: metric, At: at, Value: v}
}

func TestAnalyze(t *testing.T) {
	from := day1.Truncate(24 * time.Hour)
	to := from.Add(6 * time.Hour)
	at := func(d time.Duration) time.Time { return from.Add(d) }

	samples := []history.Sample{
		sample("outdoors", history.Temperature, at(0), 10),
		sample("hive", history.Target, at(0), 20),
		sample("hive", history.Boiler, at(time.Hour), history.BoilerOn), // A one minute run
		sample("hive", history.Boiler, at(time.Hour+time.Minute), history.BoilerIdle),
		sample("hive", history.Target, at(2*time.Hour), 16), // A setback
		sample("hive", history.Boiler, at(2*time.Hour+10*time.Minute), history.BoilerOn),
		sample("hive", history.Boiler, at(2*time.Hour+40*time.Minute), history.BoilerIdle),
		sample("hive", history.Temperature, at(2*time.Hour+45*time.Minute), 16.7),
		sample("hive", history.Boiler, at(5*time.Hour), history.BoilerOn), // Still going at the end
	}

	got := Analyze(samples, "hive", "outdoors", from, to)

	assert.Equal(t, 3, got.Starts)
	assert.Equal(t, 2, got.Complete)
	assert.Equal(t, 30*time.Minute, got.MedianRun)
	assert.Equal(t, 0.5, got.ShortShare)
	assert.Equal(t, 1, got.WorstHour)
	assert.InDelta(t, 91.0/360, got.Duty, 1e-9)
	assert.True(t, got.MildKnown)
	assert.InDelta(t, 91.0/360, got.MildDuty, 1e-9)
	assert.Equal(t, []time.Time{at(2*time.Hour + 10*time.Minute)}, got.TopUps)
	assert.InDelta(t, 0.7, got.Overshoot, 1e-9)

	assert.Equal(t, StatusOK, got.ShortStatus()) // Too few runs to grade
	assert.Equal(t, StatusOK, got.MedianStatus())
	assert.Equal(t, StatusWarn, got.TopUpStatus())
	assert.Equal(t, StatusWarn, got.OvershootStatus())

	require.Len(t, got.RunLengths, 5)
	assert.Equal(t, 1.0, got.RunLengths[0].Value)
	assert.Equal(t, 1.0, got.RunLengths[4].Value)

	require.Len(t, got.StartsByHour, 24)
	assert.Equal(t, 1.0, got.StartsByHour[1].Value)
	assert.Equal(t, 1.0, got.StartsByHour[2].Value)
	assert.Equal(t, 1.0, got.StartsByHour[5].Value)

	require.Len(t, got.DutyByOutdoors, 4)
	assert.InDelta(t, 100*91.0/360, got.DutyByOutdoors[2].Value, 1e-9)
	assert.False(t, got.DutyByOutdoors[2].Muted)
	assert.True(t, got.DutyByOutdoors[0].Muted)
}

func TestAnalyzeCarriedRun(t *testing.T) {
	from := day1.Truncate(24 * time.Hour)
	to := from.Add(4 * time.Hour)

	samples := []history.Sample{
		sample("hive", history.Boiler, from.Add(-time.Hour), history.BoilerOn), // Began before the period
		sample("hive", history.Boiler, from.Add(time.Hour), history.BoilerIdle),
	}

	got := Analyze(samples, "hive", "outdoors", from, to)

	assert.Zero(t, got.Starts)
	assert.Zero(t, got.Complete)
	assert.InDelta(t, 0.25, got.Duty, 1e-9)
	assert.False(t, got.MildKnown)
	assert.Equal(t, StatusOK, got.MedianStatus())
}

func TestAnalyzeIgnoresRunsBeforeThePeriod(t *testing.T) {
	from := day1.Truncate(24 * time.Hour)
	to := from.Add(4 * time.Hour)

	samples := []history.Sample{
		sample("hive", history.Boiler, from.Add(-2*time.Hour), history.BoilerOn),
		sample("hive", history.Boiler, from.Add(-time.Hour), history.BoilerIdle),
	}

	got := Analyze(samples, "hive", "outdoors", from, to)

	assert.Empty(t, got.Runs)
	assert.Zero(t, got.Duty)
}

func TestAnalyzeGradesNeedEnoughRuns(t *testing.T) {
	from := day1.Truncate(24 * time.Hour)
	to := from.Add(6 * time.Hour)

	var samples []history.Sample

	for i := range minRuns {
		start := from.Add(time.Duration(i) * time.Hour)

		samples = append(
			samples,
			sample("hive", history.Boiler, start, history.BoilerOn),
			sample("hive", history.Boiler, start.Add(time.Minute), history.BoilerIdle),
		)
	}

	got := Analyze(samples, "hive", "outdoors", from, to)

	assert.Equal(t, StatusBad, got.MedianStatus())
	assert.Equal(t, StatusBad, got.ShortStatus())
}

func TestAnalyzeIgnoresOtherDevicesBoiler(t *testing.T) {
	from := day1.Truncate(24 * time.Hour)
	to := from.Add(4 * time.Hour)

	samples := []history.Sample{
		sample("other", history.Boiler, from.Add(time.Hour), history.BoilerOn),
		sample("other", history.Boiler, from.Add(2*time.Hour), history.BoilerIdle),
	}

	got := Analyze(samples, "hive", "outdoors", from, to)

	assert.Empty(t, got.Runs)
}
