package hive

import (
	"fmt"
	"time"
)

// HeatSchedule follows the schedule stored on the thermostat.
func HeatSchedule(r Receiver) Action {
	mode := r.Key("system_mode")
	hold := r.Key("temperature_setpoint_hold")
	dur := r.Key("temperature_setpoint_hold_duration")

	return Action{
		Desc:   "heating: follow thermostat schedule",
		Steps:  []Step{{Payload: Payload{{mode, "heat"}, {hold, 0}, {dur, 0}}}},
		Verify: []string{mode, hold},
		Done: func(s map[string]any) bool {
			h, ok := s[hold]
			return modeIs(mode, "heat")(s) && ok && !truthy(h)
		},
	}
}

// HeatManual holds the given temperature until told otherwise.
func HeatManual(r Receiver, temp float64) Action {
	mode := r.Key("system_mode")
	setpoint := r.Key("occupied_heating_setpoint")
	hold := r.Key("temperature_setpoint_hold")
	dur := r.Key("temperature_setpoint_hold_duration")

	return Action{
		Desc: fmt.Sprintf("heating: manual, hold %s", FormatTemp(temp)),
		Steps: []Step{
			{Payload: Payload{{mode, "heat"}, {setpoint, temp}, {hold, 1}, {dur, 0}}, Pause: 500 * time.Millisecond},
			// The setpoint is sent again once the hold is in place, as the receiver can otherwise
			// keep the previous schedule target.
			{Payload: Payload{{mode, "heat"}, {setpoint, temp}}},
		},
		Verify: []string{mode, hold, setpoint},
		Done: func(s map[string]any) bool {
			t, ok := num(s[setpoint])
			return modeIs(mode, "heat")(s) && truthy(s[hold]) && ok && t == temp
		},
	}
}

// HeatOff turns the heating off; frost protection still applies.
func HeatOff(r Receiver) Action {
	mode := r.Key("system_mode")
	hold := r.Key("temperature_setpoint_hold")

	return Action{
		Desc:   "heating: off (frost protection only)",
		Steps:  []Step{{Payload: Payload{{mode, "off"}, {hold, 0}}}},
		Verify: []string{mode},
		Done:   modeIs(mode, "off"),
	}
}

// HeatBoost boosts the heating to the given temperature for the given number of minutes.
func HeatBoost(r Receiver, mins int, temp float64) Action {
	mode := r.Key("system_mode")
	setpoint := r.Key("occupied_heating_setpoint")
	hold := r.Key("temperature_setpoint_hold")
	dur := r.Key("temperature_setpoint_hold_duration")

	return Action{
		Desc:   fmt.Sprintf("heating: boost to %s for %d min", FormatTemp(temp), mins),
		Steps:  []Step{{Payload: Payload{{mode, "emergency_heating"}, {dur, mins}, {hold, 1}, {setpoint, temp}}}},
		Verify: []string{mode},
		Done:   modeIs(mode, "emergency_heating"),
	}
}
