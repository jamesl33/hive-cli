package analyze

import (
	"bytes"
	"testing"
	"time"

	"github.com/jamesl33/hive-cli/internal/analyze"
	"github.com/stretchr/testify/assert"
)

func TestWriteChecks(t *testing.T) {
	a := analyze.Analysis{
		Starts:        6,
		Complete:      5,
		MedianRun:     3*time.Minute + 20*time.Second,
		ShortShare:    0.6,
		StartsPerHour: 0.25,
		WorstHour:     4,
		Duty:          0.4,
		MildDuty:      0.5,
		MildKnown:     true,
		TopUps:        []time.Time{time.Date(2026, 10, 24, 12, 33, 0, 0, time.UTC)},
		Overshoot:     0.4,
	}

	var buf bytes.Buffer

	writeChecks(&buf, a, time.UTC)

	want := "runs                   6       started\n" +
		"median run         3m20s  warn \n" +
		"short runs           60%  bad  under 5m\n" +
		"starts/hour            4  warn worst hour · 0.2 mean\n" +
		"duty                 50%  ok   above 8°C · 40% overall\n" +
		"setback top-ups        1  warn 10-24 12:33\n" +
		"overshoot           +0.4  ok   °C over target when heated\n"

	assert.Equal(t, want, buf.String())
}

func TestWriteHistogramScalesBars(t *testing.T) {
	buckets := []analyze.Bucket{{Label: "<2m", Value: 4}, {Label: "2–5m", Value: 2}, {Label: "5–10m"}}

	var buf bytes.Buffer

	writeHistogram(&buf, "run length", buckets, "")

	want := "\nrun length\n" +
		"<2m      ██████████████████████████████ 4\n" +
		"2–5m     ███████████████                2\n" +
		"5–10m                                   0\n"

	assert.Equal(t, want, buf.String())
}

func TestDaysRejectsLastBeforeFirst(t *testing.T) {
	_, _, err := days(time.Now(), []string{"2026-10-25", "2026-10-24"})

	assert.Error(t, err)
}
