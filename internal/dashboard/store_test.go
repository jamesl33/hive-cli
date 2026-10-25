package dashboard

import (
	"path/filepath"
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

func openLog(t *testing.T) (*history.Log, string) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "history")
	l, err := history.OpenLog(dir)
	require.NoError(t, err)

	return l, dir
}

func TestStoreRestoreAndReloadByWindow(t *testing.T) {
	l, dir := openLog(t)

	late := day1.Add(10 * 24 * time.Hour)

	l.Add(sample("office", history.Temperature, day1, 19))
	l.Add(sample("office", history.Temperature, late, 21))
	require.NoError(t, l.Flush(late))

	s := NewStore(receiver)
	require.NoError(t, s.Restore(dir, late))

	// Only the last day: the checkpoint which starts it, and the change
	series := s.Series[history.Temperature]["office"]
	require.Len(t, series, 2)
	assert.Equal(t, 19.0, series[0].Value)
	assert.Equal(t, 21.0, series[1].Value)
	assert.Equal(t, []string{"office"}, s.Order)

	// A wider window loads the first day too
	require.NoError(t, s.Reload(day1))
	assert.Len(t, s.Series[history.Temperature]["office"], 3)

	// A sample which is not written yet survives a reload
	s.add(sample("office", history.Temperature, late.Add(time.Hour), 22))
	require.NoError(t, s.Reload(late))
	assert.Len(t, s.Series[history.Temperature]["office"], 3)
	assert.Equal(t, 22.0, s.Series[history.Temperature]["office"][2].Value)
}

func TestVisible(t *testing.T) {
	s := Series{{At: day1, Value: 1}, {At: day1.Add(time.Hour), Value: 2}, {At: day1.Add(2 * time.Hour), Value: 3}}

	assert.Equal(t, s, visible(s, day1.Add(-time.Hour)))
	assert.Equal(t, s[1:], visible(s, day1.Add(90*time.Minute)))
	assert.Equal(t, s[2:], visible(s, day1.Add(3*time.Hour)))
	assert.Empty(t, visible(nil, day1))
}
