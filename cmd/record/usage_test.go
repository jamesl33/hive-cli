package record

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jamesl33/hive-cli/internal/dashboard"
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

	log, err := dashboard.OpenLog(dir)
	require.NoError(t, err)

	c := octopus.Client{BaseURL: srv.URL, Key: "k", Fuel: octopus.Gas, Point: "1", Serial: "2", Units: octopus.KWh}

	after, err := recordUsage(t.Context(), &c, log, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, start.Add(time.Hour), after)

	// The same readings again add nothing
	again, err := recordUsage(t.Context(), &c, log, after)
	require.NoError(t, err)
	assert.Equal(t, after, again)

	require.NoError(t, log.Flush(time.Now()))

	// A repeated value is dropped, so the third half hour holds the second's
	samples, err := dashboard.Load(dir, start)
	require.NoError(t, err)
	require.Len(t, samples, 2)
	assert.Equal(t, dashboard.Gas, samples[0].Metric)
	assert.Equal(t, 0.25, samples[0].Value)
	assert.Equal(t, 0.5, samples[1].Value)

	latest, err := latestUsage(dir, dashboard.Gas)
	require.NoError(t, err)
	assert.Equal(t, start.Add(30*time.Minute), latest)
}

func TestMeterOptionsClient(t *testing.T) {
	_, err := meterOptions{fuel: octopus.Gas, point: "1"}.client("")
	assert.Error(t, err)

	c, err := meterOptions{fuel: octopus.Gas}.client("")
	assert.NoError(t, err)
	assert.Nil(t, c)

	c, err = meterOptions{fuel: octopus.Gas, point: "1", serial: "2"}.client("k")
	require.NoError(t, err)
	assert.Equal(t, "1", c.Point)
	assert.Equal(t, octopus.M3, c.Units)

	c, err = meterOptions{fuel: octopus.Electricity, point: "3", serial: "4"}.client("k")
	require.NoError(t, err)
	assert.Equal(t, octopus.Electricity, c.Fuel)
}

func TestRecordUsageElectricity(t *testing.T) {
	start := time.Now().UTC().Truncate(30 * time.Minute).Add(-time.Hour)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/electricity-meter-points/")
		fmt.Fprintf(w, `{"results":[{"consumption":0.3,"interval_start":%q}]}`, start.Format(time.RFC3339))
	}))
	defer srv.Close()

	dir := t.TempDir()

	log, err := dashboard.OpenLog(dir)
	require.NoError(t, err)

	c := octopus.Client{BaseURL: srv.URL, Key: "k", Fuel: octopus.Electricity, Point: "1", Serial: "2"}

	_, err = recordUsage(t.Context(), &c, log, time.Time{})
	require.NoError(t, err)
	require.NoError(t, log.Flush(time.Now()))

	samples, err := dashboard.Load(dir, start)
	require.NoError(t, err)
	require.Len(t, samples, 1)
	assert.Equal(t, dashboard.Electricity, samples[0].Metric)
	assert.Equal(t, "electricity", samples[0].Device)
	assert.Equal(t, 0.3, samples[0].Value)
}
