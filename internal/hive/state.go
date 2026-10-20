package hive

import (
	"fmt"
	"strconv"
	"strings"
)

// Reported modes of a channel.
const (
	ModeSchedule = "schedule"
	ModeManual   = "manual"
	ModeOff      = "off"
	ModeBoost    = "boost"
	ModeUnknown  = "unknown"
)

// boostGarbage is the remaining duration above which a value is ignored; during boost some
// receivers report a nonsense remaining duration (65535 or close to it) until the next report.
const boostGarbage = 65000

// State is the interpreted state of the heating.
type State struct {
	// Mode is one of the Mode* constants.
	Mode string

	// Target is the target temperature (heating only).
	Target *float64

	// Temperature is the room temperature (heating only).
	Temperature *float64

	// Active is true while the boiler is firing for this channel.
	Active *bool

	// BoostLeft is the number of minutes remaining on a boost.
	BoostLeft *int
}

// Interpret converts the raw attributes reported by the receiver into the state of the heating.
func Interpret(r Receiver, state map[string]any) State {
	k := r.Key

	var cs State

	mode, _ := state[k("system_mode")].(string)

	switch mode {
	case "off":
		cs.Mode = ModeOff
	case "emergency_heating":
		cs.Mode = ModeBoost
		cs.BoostLeft = boostLeft(state, k)
	case "heat":
		cs.Mode = heldMode(state, k)
	default:
		cs.Mode = ModeUnknown
	}

	if rs, ok := state[k("running_state")].(string); ok {
		active := rs == "heat"
		cs.Active = &active
	}

	// The receiver reports a setpoint of 1 when it is off; that is a placeholder, not a real
	// target.
	if t, ok := num(state[k("occupied_heating_setpoint")]); ok && t > 1 {
		cs.Target = &t
	}

	if t, ok := num(state[k("local_temperature")]); ok {
		cs.Temperature = &t
	}

	return cs
}

// StatusKeys returns the properties which must be read to interpret the state of the receiver.
func StatusKeys(r Receiver) []string {
	keys := make([]string, 0, 6)

	for _, attr := range []string{
		"system_mode",
		"occupied_heating_setpoint",
		"local_temperature",
		"running_state",
		"temperature_setpoint_hold",
		"temperature_setpoint_hold_duration",
	} {
		keys = append(keys, r.Key(attr))
	}

	return keys
}

// String formats the state for humans.
func (c State) String() string {
	var b strings.Builder

	fmt.Fprintf(&b, "Heating: %s", c.Mode)

	if c.Target != nil && c.Mode != ModeOff {
		fmt.Fprintf(&b, ", target %s", FormatTemp(*c.Target))
	}

	if c.BoostLeft != nil {
		fmt.Fprintf(&b, ", %d min left", *c.BoostLeft)
	}

	if c.Temperature != nil {
		fmt.Fprintf(&b, ", room %s", FormatTemp(*c.Temperature))
	}

	if c.Active != nil {
		b.WriteString(activity(*c.Active))
	}

	return b.String()
}

// boostLeft returns the minutes remaining on a boost, or nil if the receiver reported nonsense.
func boostLeft(state map[string]any, k func(string) string) *int {
	d, ok := num(state[k("temperature_setpoint_hold_duration")])
	if !ok || d >= boostGarbage {
		return nil
	}

	n := int(d)

	return &n
}

// heldMode returns the mode of a receiver which is heating: manual if a temperature is being held,
// otherwise following the schedule.
func heldMode(state map[string]any, k func(string) string) string {
	if truthy(state[k("temperature_setpoint_hold")]) {
		return ModeManual
	}

	return ModeSchedule
}

// activity describes whether the boiler is firing.
func activity(active bool) string {
	if active {
		return ", boiler on"
	}

	return ", idle"
}

func truthy(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		return x == "1" || strings.EqualFold(x, "true") || strings.EqualFold(x, "on")
	}

	return false
}

func num(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	}

	return 0, false
}
