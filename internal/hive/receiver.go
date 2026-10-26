// Package hive implements Hive receiver semantics on top of Zigbee2MQTT.
//
// The SLT3c thermostat is only a remote control: it pairs directly with the receiver (SLR1c
// heating-only, or SLR2c heating + hot water), and the receiver is the device Zigbee2MQTT exposes. Hot water is not supported.
// Hive encodes its modes in a combination of attributes, which Zigbee2MQTT's generic climate
// controls do not get right:
//
//	schedule  system_mode=heat                temperature_setpoint_hold=0
//	manual    system_mode=heat                temperature_setpoint_hold=1, duration 0
//	off       system_mode=off                 temperature_setpoint_hold=0
//	boost     system_mode=emergency_heating   temperature_setpoint_hold=1, duration=minutes
//
// These payloads follow the community Home Assistant integration
// andrew-codechimp/HA-Hive-Local-Thermostat (tested on SLR1c, SLR2c, SLR2d).
package hive

import "strings"

// Receiver is a Hive receiver as known to Zigbee2MQTT.
type Receiver struct {
	// Name is the Zigbee2MQTT friendly name.
	Name string

	// Model is the device model e.g. SLR1c, SLR2c.
	Model string
}

// Dual returns true if the receiver exposes its attributes with a _heat suffix (SLR2 models, which
// also have a hot water channel).
func (r Receiver) Dual() bool {
	return strings.HasPrefix(strings.ToUpper(r.Model), "SLR2")
}

// Key maps an attribute to its Zigbee2MQTT property.
func (r Receiver) Key(attr string) string {
	if r.Dual() {
		return attr + "_heat"
	}

	return attr
}

// IsReceiverModel returns true if the model is a Hive receiver (rather than a thermostat).
func IsReceiverModel(model string) bool {
	return strings.HasPrefix(strings.ToUpper(model), "SLR")
}

// IsThermostatModel returns true if the model is a Hive thermostat, which is paired with the receiver.
func IsThermostatModel(model string) bool {
	return strings.HasPrefix(strings.ToUpper(model), "SLT")
}
