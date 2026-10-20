package z2m

import "os"

// Options defines the options shared by every command which talks to Zigbee2MQTT.
type Options struct {
	// Broker is the MQTT broker URL; tcp:// or mqtts://.
	Broker string

	// Username is the MQTT username.
	Username string

	// Password is the MQTT password.
	Password string
}

// Env returns the value of the environment variable, or the default if it's not set.
func Env(name, def string) string {
	if v, ok := os.LookupEnv(name); ok {
		return v
	}

	return def
}
