package dashboard

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var day1 = time.Date(2026, 10, 22, 10, 0, 0, 0, time.UTC)

func sample(device string, metric Metric, at time.Time, v float64) Sample {
	return Sample{Device: device, Metric: metric, At: at, Value: v}
}

func openLog(t *testing.T) (*Log, string) {
	t.Helper()

	dir := filepath.Join(t.TempDir(), "history")
	l, err := OpenLog(dir)
	require.NoError(t, err)

	return l, dir
}

func TestLogDropsUnchanged(t *testing.T) {
	l, dir := openLog(t)

	l.Add(sample("office", Temperature, day1, 19.21))
	l.Add(sample("office", Temperature, day1.Add(time.Minute), 19.24)) // Rounds to the same
	l.Add(sample("office", Temperature, day1.Add(2*time.Minute), 19.3))
	l.Add(sample("office", Humidity, day1.Add(3*time.Minute), 48.4))
	require.NoError(t, l.Flush(day1.Add(4*time.Minute)))

	got, err := Load(dir, day1)
	require.NoError(t, err)

	require.Len(t, got, 3)
	assert.Equal(t, 19.2, got[0].Value)
	assert.Equal(t, 19.3, got[1].Value)
	assert.Equal(t, Humidity, got[2].Metric)
	assert.Equal(t, 48.0, got[2].Value)
}

func TestLogWritesOnlyOnFlush(t *testing.T) {
	l, dir := openLog(t)

	l.Add(sample("office", Battery, day1, 90))

	got, err := Load(dir, day1)
	require.NoError(t, err)
	assert.Empty(t, got)

	require.NoError(t, l.Flush(day1))

	got, err = Load(dir, day1)
	require.NoError(t, err)
	assert.Len(t, got, 1)
}

func TestLogStartsEachDayWithEverySeries(t *testing.T) {
	l, dir := openLog(t)
	day2 := day1.Add(24 * time.Hour)

	l.Add(sample("office", Battery, day1, 90))
	l.Add(sample("office", Temperature, day1, 19))
	l.Add(sample("office", Temperature, day2, 20))
	require.NoError(t, l.Flush(day2))

	got, err := Load(dir, day2)
	require.NoError(t, err)

	midnight := day2.Truncate(24 * time.Hour)

	require.Len(t, got, 3)
	assert.Equal(t, sample("office", Battery, midnight, 90), got[0])
	assert.Equal(t, sample("office", Temperature, midnight, 19), got[1])
	assert.Equal(t, sample("office", Temperature, day2, 20), got[2])
}

func TestLogFlushStartsDayWithoutChanges(t *testing.T) {
	l, dir := openLog(t)
	day3 := day1.Add(48 * time.Hour)

	l.Add(sample("office", Battery, day1, 90))
	require.NoError(t, l.Flush(day3))

	got, err := Load(dir, day3)
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, 90.0, got[0].Value)
	assert.Equal(t, day3.Truncate(24*time.Hour), got[0].At)
}

func TestLogReopenKeepsLastValues(t *testing.T) {
	l, dir := openLog(t)

	l.Add(sample("office", Battery, day1, 90))
	require.NoError(t, l.Flush(day1))

	l, err := OpenLog(dir)
	require.NoError(t, err)

	l.Add(sample("office", Battery, day1.Add(time.Hour), 90))
	l.Add(sample("office", Battery, day1.Add(2*time.Hour), 89))
	require.NoError(t, l.Flush(day1.Add(3*time.Hour)))

	got, err := Load(dir, day1)
	require.NoError(t, err)

	require.Len(t, got, 2)
	assert.Equal(t, 89.0, got[1].Value)
}

func TestLogPrunesOldDays(t *testing.T) {
	l, dir := openLog(t)

	l.Add(sample("office", Battery, day1, 90))
	require.NoError(t, l.Flush(day1))

	// The 29th and 31st days after
	require.NoError(t, l.Flush(day1.Add(29*24*time.Hour)))
	assert.FileExists(t, filepath.Join(dir, "2026-10-22.log"))

	require.NoError(t, l.Flush(day1.Add(31*24*time.Hour)))
	assert.NoFileExists(t, filepath.Join(dir, "2026-10-22.log"))
	assert.FileExists(t, filepath.Join(dir, "2026-11-22.log"))
}

func TestLogTimeGoingBackIsRecordedInCurrentDay(t *testing.T) {
	l, dir := openLog(t)

	l.Add(sample("office", Battery, day1, 90))
	l.Add(sample("office", Battery, day1.Add(-48*time.Hour), 80))
	require.NoError(t, l.Flush(day1))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestLoadSkipsEarlierDaysAndBadLines(t *testing.T) {
	dir := t.TempDir()

	write := func(name, content string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}

	write("2026-10-20.log", "1 battery 50 old\n")
	write("2026-10-21.log", "1792569600 temperature 195 Temperature/Humidity (Office)\nnonsense\n1792569700 temperature 19")
	write("notes.txt", "ignored")

	got, err := Load(dir, time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	require.Len(t, got, 1)
	assert.Equal(t, sample("Temperature/Humidity (Office)", Temperature, time.Unix(1792569600, 0).UTC(), 19.5), got[0])
}

func TestLoadSkipsNULBytes(t *testing.T) {
	dir := t.TempDir()
	day := day1.Truncate(24 * time.Hour)
	content := "1 temperature 150 office\n1 battery 90 office\x00\x00\x00\x002 temperature 160 office\n\x00\x00\x003 temperature 170 office\n"
	require.NoError(t, os.WriteFile(dayPath(dir, day), []byte(content), 0o644))

	got, err := Load(dir, day)
	require.NoError(t, err)

	require.Len(t, got, 4)
	assert.Equal(t, 16.0, got[2].Value)
	assert.Equal(t, 17.0, got[3].Value)
}

func TestLoadMissingDirectory(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "missing"), day1)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestStoreRestoreAndReloadByWindow(t *testing.T) {
	l, dir := openLog(t)

	late := day1.Add(10 * 24 * time.Hour)

	l.Add(sample("office", Temperature, day1, 19))
	l.Add(sample("office", Temperature, late, 21))
	require.NoError(t, l.Flush(late))

	s := NewStore(receiver)
	require.NoError(t, s.Restore(dir, late))

	// Only the last day: the checkpoint which starts it, and the change
	series := s.Series[Temperature]["office"]
	require.Len(t, series, 2)
	assert.Equal(t, 19.0, series[0].Value)
	assert.Equal(t, 21.0, series[1].Value)
	assert.Equal(t, []string{"office"}, s.Order)

	// A wider window loads the first day too
	require.NoError(t, s.Reload(day1))
	assert.Len(t, s.Series[Temperature]["office"], 3)

	// A sample which is not written yet survives a reload
	s.add(sample("office", Temperature, late.Add(time.Hour), 22))
	require.NoError(t, s.Reload(late))
	assert.Len(t, s.Series[Temperature]["office"], 3)
	assert.Equal(t, 22.0, s.Series[Temperature]["office"][2].Value)
}

func TestVisible(t *testing.T) {
	s := Series{{At: day1, Value: 1}, {At: day1.Add(time.Hour), Value: 2}, {At: day1.Add(2 * time.Hour), Value: 3}}

	assert.Equal(t, s, visible(s, day1.Add(-time.Hour)))
	assert.Equal(t, s[1:], visible(s, day1.Add(90*time.Minute)))
	assert.Equal(t, s[2:], visible(s, day1.Add(3*time.Hour)))
	assert.Empty(t, visible(nil, day1))
}
