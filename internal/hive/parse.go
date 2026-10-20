package hive

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// FormatTemp formats a temperature for humans.
func FormatTemp(t float64) string {
	return strconv.FormatFloat(t, 'f', -1, 64) + "°C"
}

// ParseTemp validates a setpoint: 5–32 °C in 0.5 steps (the receiver's range).
func ParseTemp(s string) (float64, error) {
	t, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSuffix(s, "C"), "°"), 64)
	if err != nil || math.IsNaN(t) {
		return 0, fmt.Errorf("bad temperature %q", s)
	}

	if t < 5 || t > 32 {
		return 0, fmt.Errorf("temperature %s is outside 5–32°C", s)
	}

	return math.Round(t*2) / 2, nil
}

// ParseMinutes accepts 90, 90m, 1h, 1h30m. Boosts are limited to 1–360 minutes, matching what the
// Hive thermostat itself offers.
func ParseMinutes(s string) (int, error) {
	mins, err := minutes(s)
	if err != nil {
		return 0, err
	}

	if mins < 1 || mins > 360 {
		return 0, fmt.Errorf("boost duration must be 1–360 minutes, got %d", mins)
	}

	return mins, nil
}

// minutes converts a bare number of minutes or a duration such as 1h30m to minutes.
func minutes(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err == nil {
		return n, nil
	}

	d, err := time.ParseDuration(s)
	if err != nil || d%time.Minute != 0 {
		return 0, fmt.Errorf("bad duration %q (try 30, 45m, 1h30m)", s)
	}

	return int(d / time.Minute), nil
}
