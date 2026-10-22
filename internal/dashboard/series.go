// Package dashboard is a live terminal dashboard of the heating and the temperature sensors.
package dashboard

import "time"

const (
	// maxAge is how long samples are kept.
	maxAge = 24 * time.Hour

	// maxPoints is the most samples kept for a series.
	maxPoints = 10000
)

// Point is a value at a point in time.
type Point struct {
	At    time.Time
	Value float64
}

// Series is a sequence of points in chronological order.
type Series []Point

// Append adds a point, dropping the points which are too old or exceed the limit.
func (s Series) Append(p Point) Series {
	s = append(s, p)

	cut := 0
	for cut < len(s)-1 && p.At.Sub(s[cut].At) > maxAge {
		cut++
	}

	if over := len(s) - cut - maxPoints; over > 0 {
		cut += over
	}

	return s[cut:]
}

// Last returns the most recent point.
func (s Series) Last() (Point, bool) {
	if len(s) == 0 {
		return Point{}, false
	}

	return s[len(s)-1], true
}
