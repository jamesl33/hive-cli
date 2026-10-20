package hive

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTemp(t *testing.T) {
	for in, want := range map[string]float64{"20": 20, "20.3": 20.5, "19.2": 19, "21°C": 21, "5": 5, "32": 32} {
		got, err := ParseTemp(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}

	for _, in := range []string{"4.5", "33", "warm", ""} {
		_, err := ParseTemp(in)
		assert.Error(t, err, in)
	}
}

func TestParseMinutes(t *testing.T) {
	for in, want := range map[string]int{"30": 30, "45m": 45, "1h": 60, "1h30m": 90, "6h": 360} {
		got, err := ParseMinutes(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}

	for _, in := range []string{"0", "361", "7h", "90s", "soon"} {
		_, err := ParseMinutes(in)
		assert.Error(t, err, in)
	}
}
