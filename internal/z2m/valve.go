package z2m

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"math"
)

// SetOpening tells a radiator valve to open to the percent, then asks it to report its opening so the
// change shows up in the feed. The valve is told both the opening to use while heating and while idle,
// since it does not report which one it follows. The valve only moves to an opening while its mode is
// off, so every write sets that too. Battery valves apply this late.
func (f *Feed) SetOpening(_ context.Context, valve string, percent float64) error {
	if !f.names[valve] {
		return fmt.Errorf("unknown device %q", valve)
	}

	set, err := valvePayload(percent)
	if err != nil {
		return err
	}

	err = f.client.Publish(baseTopic+"/"+valve+"/set", set)
	if err != nil {
		return err
	}

	get, err := json.Marshal(map[string]string{"heating_valve_position": "", "idle_valve_position": ""})
	if err != nil {
		return err
	}

	return f.client.Publish(baseTopic+"/"+valve+"/get", get)
}

// valvePayload returns the payload which sets the opening of a radiator valve.
func valvePayload(percent float64) ([]byte, error) {
	open := int(math.Round(math.Min(100, math.Max(0, percent))))

	p := map[string]any{"system_mode": "off", "heating_valve_position": open, "idle_valve_position": open}

	return json.Marshal(p)
}
