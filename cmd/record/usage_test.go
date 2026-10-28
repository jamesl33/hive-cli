package record

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jamesl33/hive-cli/internal/history"
	"github.com/jamesl33/hive-cli/internal/octopus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordUsage(t *testing.T) {
	start := time.Now().UTC().Truncate(30 * time.Minute).Add(-3 * time.Hour)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"results":[`+
			`{"consumption":0.25,"interval_start":%q},`+
			`{"consumption":0.5,"interval_start":%q},`+
			`{"consumption":0.5,"interval_start":%q}]}`,
			start.Format(time.RFC3339),
			start.Add(30*time.Minute).Format(time.RFC3339),
			start.Add(time.Hour).Format(time.RFC3339))
	}))
	defer srv.Close()

	dir := t.TempDir()

	log, err := history.OpenLog(dir)
	require.NoError(t, err)

	c := octopus.Client{BaseURL: srv.URL, Key: "k", Fuel: octopus.FuelGas, Point: "1", Serial: "2", Units: octopus.UnitsKWh}

	seen := make(map[int64]bool)

	require.NoError(t, recordUsage(t.Context(), &c, log, seen))
	assert.Len(t, seen, 3)

	// The same readings again add nothing
	require.NoError(t, recordUsage(t.Context(), &c, log, seen))

	require.NoError(t, log.Flush(time.Now()))

	// A repeated value is another half hour, so it is kept
	samples, err := history.Load(dir, start)
	require.NoError(t, err)
	require.Len(t, samples, 3)
	assert.Equal(t, history.Gas, samples[0].Metric)
	assert.Equal(t, 0.25, samples[0].Value)
	assert.Equal(t, 0.5, samples[1].Value)
	assert.Equal(t, 0.5, samples[2].Value)

	recorded, err := recordedUsage(dir, history.Gas)
	require.NoError(t, err)
	assert.Equal(t, map[int64]bool{start.Unix(): true, start.Add(30 * time.Minute).Unix(): true, start.Add(time.Hour).Unix(): true}, recorded)
}

func TestRecordUsageOutOfOrder(t *testing.T) {
	start := time.Now().UTC().Truncate(30 * time.Minute).Add(-3 * time.Hour)

	// The API does not always return readings oldest first, and repeats are readings too
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{"results":[`+
			`{"consumption":0.5,"interval_start":%q},`+
			`{"consumption":0.25,"interval_start":%q},`+
			`{"consumption":0.5,"interval_start":%q}]}`,
			start.Add(time.Hour).Format(time.RFC3339),
			start.Format(time.RFC3339),
			start.Add(30*time.Minute).Format(time.RFC3339))
	}))
	defer srv.Close()

	dir := t.TempDir()

	log, err := history.OpenLog(dir)
	require.NoError(t, err)

	c := octopus.Client{BaseURL: srv.URL, Key: "k", Fuel: octopus.FuelGas, Point: "1", Serial: "2", Units: octopus.UnitsKWh}

	require.NoError(t, recordUsage(t.Context(), &c, log, make(map[int64]bool)))
	require.NoError(t, log.Flush(time.Now()))

	samples, err := history.Load(dir, start)
	require.NoError(t, err)
	require.Len(t, samples, 3)
}

func TestMeterOptionsClient(t *testing.T) {
	_, err := meterOptions{fuel: octopus.FuelGas, point: "1"}.client("")
	assert.Error(t, err)

	c, err := meterOptions{fuel: octopus.FuelGas}.client("")
	assert.NoError(t, err)
	assert.Nil(t, c)

	c, err = meterOptions{fuel: octopus.FuelGas, point: "1", serial: "2"}.client("k")
	require.NoError(t, err)
	assert.Equal(t, "1", c.Point)
	assert.Equal(t, octopus.UnitsM3, c.Units)
	assert.NotZero(t, c.CalorificValue)

	c, err = meterOptions{fuel: octopus.FuelElectricity, point: "3", serial: "4"}.client("k")
	require.NoError(t, err)
	assert.Equal(t, octopus.FuelElectricity, c.Fuel)
	assert.Zero(t, c.CalorificValue)
}

func TestRecordUsageLateHalfHour(t *testing.T) {
	start := time.Now().UTC().Truncate(30 * time.Minute).Add(-3 * time.Hour)

	results := []time.Time{start.Add(30 * time.Minute)}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var body []string
		for _, r := range results {
			body = append(body, fmt.Sprintf(`{"consumption":0.5,"interval_start":%q}`, r.Format(time.RFC3339)))
		}

		fmt.Fprintf(w, `{"results":[%s]}`, strings.Join(body, ","))
	}))
	defer srv.Close()

	log, err := history.OpenLog(t.TempDir())
	require.NoError(t, err)

	c := octopus.Client{BaseURL: srv.URL, Key: "k", Fuel: octopus.FuelGas, Point: "1", Serial: "2", Units: octopus.UnitsKWh}
	seen := make(map[int64]bool)

	require.NoError(t, recordUsage(t.Context(), &c, log, seen))

	// An earlier half hour published afterwards is still picked up
	results = append(results, start)

	require.NoError(t, recordUsage(t.Context(), &c, log, seen))
	assert.Len(t, seen, 2)
}

func TestRecordUsageElectricity(t *testing.T) {
	start := time.Now().UTC().Truncate(30 * time.Minute).Add(-time.Hour)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/electricity-meter-points/")
		fmt.Fprintf(w, `{"results":[{"consumption":0.3,"interval_start":%q}]}`, start.Format(time.RFC3339))
	}))
	defer srv.Close()

	dir := t.TempDir()

	log, err := history.OpenLog(dir)
	require.NoError(t, err)

	c := octopus.Client{BaseURL: srv.URL, Key: "k", Fuel: octopus.FuelElectricity, Point: "1", Serial: "2"}

	require.NoError(t, recordUsage(t.Context(), &c, log, make(map[int64]bool)))
	require.NoError(t, log.Flush(time.Now()))

	samples, err := history.Load(dir, start)
	require.NoError(t, err)
	require.Len(t, samples, 1)
	assert.Equal(t, history.Electricity, samples[0].Metric)
	assert.Equal(t, "electricity", samples[0].Device)
	assert.Equal(t, 0.3, samples[0].Value)
}

func TestUsageFrom(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	assert.True(t, now.Add(-usageBackfill).Equal(usageFrom(map[int64]bool{}, now)))

	// After an outage longer than the refetch the fetch starts from the newest half hour recorded
	old := now.Add(-10 * 24 * time.Hour)
	seen := map[int64]bool{old.Unix(): true, old.Add(-time.Hour).Unix(): true}
	assert.True(t, old.Add(-usageRefetch).Equal(usageFrom(seen, now)))

	// Half hours before the fetch are forgotten
	seen = map[int64]bool{now.Unix(): true, now.Add(-usageRefetch - time.Hour).Unix(): true}
	usageFrom(seen, now)
	assert.Equal(t, map[int64]bool{now.Unix(): true}, seen)
}
