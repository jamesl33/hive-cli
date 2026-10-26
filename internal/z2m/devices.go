package z2m

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jamesl33/hive-cli/internal/hive"
	"github.com/jamesl33/hive-cli/internal/mqtt"
)

const (
	// baseTopic is the Zigbee2MQTT base topic.
	baseTopic = "zigbee2mqtt"

	// timeout is how long to wait before giving up.
	timeout = 15 * time.Second
)

// Definition describes what kind of device a Device is.
type Definition struct {
	// Model is the device model, e.g. SLR1c.
	Model string `json:"model"`

	// Vendor is the device manufacturer.
	Vendor string `json:"vendor"`
}

// Device is a device known to Zigbee2MQTT.
type Device struct {
	// FriendlyName is the name used in the device's topics.
	FriendlyName string `json:"friendly_name"`

	// Definition is nil until Zigbee2MQTT has interviewed the device.
	Definition *Definition `json:"definition"`
}

// IsReceiver returns true if the device is a Hive receiver.
func (d Device) IsReceiver() bool {
	return d.Definition != nil && strings.EqualFold(d.Definition.Vendor, "Hive") &&
		hive.IsReceiverModel(d.Definition.Model)
}

// IsThermostat returns true if the device is a Hive thermostat.
func (d Device) IsThermostat() bool {
	return d.Definition != nil && strings.EqualFold(d.Definition.Vendor, "Hive") &&
		hive.IsThermostatModel(d.Definition.Model)
}

// Devices returns the devices known to Zigbee2MQTT.
func Devices(ctx context.Context, c *mqtt.Client) ([]Device, error) {
	topic := baseTopic + "/bridge/devices"

	err := c.Subscribe(topic)
	if err != nil {
		return nil, err
	}

	for {
		m, err := next(ctx, c, topic)
		if err != nil {
			return nil, err
		}

		return parseDevices(m)
	}
}

// next returns the next message, explaining a timeout in terms of the device list.
func next(ctx context.Context, c *mqtt.Client, topic string) (mqtt.Message, error) {
	m, err := c.Next(ctx, 0)
	if errors.Is(err, mqtt.ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
		return m, fmt.Errorf("no device list on %s within %s: is Zigbee2MQTT running?", topic, timeout)
	}

	return m, err
}

// parseDevices parses the device list published by Zigbee2MQTT.
func parseDevices(m mqtt.Message) ([]Device, error) {
	var devices []Device

	err := json.Unmarshal(m.Payload, &devices)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", m.Topic, err)
	}

	return devices, nil
}

// pickReceiver selects the only Hive receiver, failing if there isn't exactly one.
func pickReceiver(devices []Device) (hive.Receiver, error) {
	var found []hive.Receiver

	for _, d := range devices {
		if d.IsReceiver() {
			found = append(found, hive.Receiver{Name: d.FriendlyName, Model: d.Definition.Model})
		}
	}

	switch len(found) {
	case 0:
		return hive.Receiver{}, errors.New("no Hive receiver found in Zigbee2MQTT; pair the receiver (SLR1c/SLR2c) first")
	case 1:
		return found[0], nil
	default:
		return hive.Receiver{}, fmt.Errorf("found %d Hive receivers; only one is supported", len(found))
	}
}
