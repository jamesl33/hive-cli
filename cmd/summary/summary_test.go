package summary

import (
	"bytes"
	"testing"
	"time"

	"github.com/jamesl33/hive-cli/internal/dashboard"
	"github.com/jamesl33/hive-cli/internal/history"
	"github.com/stretchr/testify/assert"
)

func TestWriteFooterAlignsFuels(t *testing.T) {
	sum := dashboard.Summary{
		SpreadMean: 2.3,
		SpreadMax:  3.9,
		BoilerOn:   3*time.Hour + 16*time.Minute,
		BoilerRuns: 6,
		Fuels:      map[history.Metric]float64{history.Gas: 24.5, history.Electricity: 0.3},
	}

	var buf bytes.Buffer

	writeFooter(&buf, sum)

	want := "\n" +
		"spread        2.3°C mean · 3.9°C max\n" +
		"boiler        3h16m on · 6 runs\n" +
		"\n" +
		"gas            24.5 kWh\n" +
		"electricity     0.3 kWh\n"

	assert.Equal(t, want, buf.String())
}

func TestWriteGroupsDevices(t *testing.T) {
	sum := dashboard.Summary{
		Groups: []dashboard.Group{
			{Name: "Sensors", Devices: []dashboard.DeviceSummary{{Device: "tah_lounge", Room: true, Mean: 16.5, Min: 16, Max: 17}}},
			{Name: "TRVs", Devices: []dashboard.DeviceSummary{{Device: "trv_lounge", Mean: 40, Min: 39, Max: 41}}},
		},
	}

	var buf bytes.Buffer

	write(&buf, sum, "tah_outdoors", time.Unix(0, 0).UTC(), time.Unix(0, 0).UTC().Add(24*time.Hour))

	out := buf.String()

	assert.Contains(t, out, "Sensors\n  lounge")
	assert.Contains(t, out, "max\nSensors\n  lounge")
	assert.Contains(t, out, "\n\nTRVs\n  lounge")
	assert.NotContains(t, out, "tah_")
	assert.NotContains(t, out, "trv_")
}
