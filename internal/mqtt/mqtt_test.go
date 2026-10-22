package mqtt

import (
	paho "github.com/eclipse/paho.mqtt.golang"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBrokerURL(t *testing.T) {
	for in, want := range map[string]string{
		"localhost":           "tcp://localhost:1883",
		"mqtts://broker.lan":  "ssl://broker.lan:8883",
		"tcp://10.0.0.2:1884": "tcp://10.0.0.2:1884",
		"tcp://[::1]":         "tcp://[::1]:1883",
	} {
		got, err := brokerURL(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}
}

func TestBrokerURLInvalid(t *testing.T) {
	for _, in := range []string{"http://broker", "tcp://"} {
		_, err := brokerURL(in)
		assert.Error(t, err, in)
	}
}

func TestCloseTwice(t *testing.T) {
	c := &Client{stop: make(chan struct{}), client: paho.NewClient(paho.NewClientOptions())}

	assert.NotPanics(t, func() {
		c.Close()
		c.Close()
	})
}
