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

func TestRecordGas(t *testing.T) {
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

	c := octopus.Client{BaseURL: srv.URL, Key: "k", MPRN: "1", Serial: "2", Units: octopus.KWh}

	after, err := recordGas(t.Context(), &c, log, time.Time{})
	require.NoError(t, err)
	assert.Equal(t, start.Add(time.Hour), after)

	// The same readings again add nothing
	again, err := recordGas(t.Context(), &c, log, after)
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

	latest, err := latestGas(dir)
	require.NoError(t, err)
	assert.Equal(t, start.Add(30*time.Minute), latest)
}

func TestGasOptionsClient(t *testing.T) {
	_, err := gasOptions{mprn: "1", units: "m3"}.client("")
	assert.Error(t, err)

	c, err := gasOptions{units: "m3"}.client("")
	assert.NoError(t, err)
	assert.Nil(t, c)

	_, err = gasOptions{mprn: "1", serial: "2", units: "gallons"}.client("k")
	assert.Error(t, err)

	c, err = gasOptions{mprn: "1", serial: "2", units: "m3", cv: 39.5}.client("k")
	require.NoError(t, err)
	assert.Equal(t, "1", c.MPRN)
}
