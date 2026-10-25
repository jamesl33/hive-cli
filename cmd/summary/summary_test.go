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
