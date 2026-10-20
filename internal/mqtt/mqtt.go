// Package mqtt wraps the Paho MQTT client with the small API a one-shot CLI needs: connect,
// subscribe, publish and receive, all bounded by a single deadline.
package mqtt

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

var (
	// ErrTimeout is returned once the overall deadline passes.
	ErrTimeout = errors.New("timed out")

	// ErrQuiet is returned when no message arrives within the requested wait window.
	ErrQuiet = errors.New("no message within wait window")
)

// Message is a message received from the broker.
type Message struct {
	Topic   string
	Payload []byte

	// Retained is true for a stored message replayed by the broker on subscribe, rather than a live one.
	Retained bool
}

// Client is a connection to an MQTT broker.
type Client struct {
	client   paho.Client
	msgs     chan Message
	done     chan struct{} // closed when the connection is lost
	stop     chan struct{} // closed by Close
	err      error         // why the connection was lost; only valid once done is closed
	deadline time.Time
}

// brokerURL normalizes a broker address; tcp://, mqtts:// or a bare host[:port], to the
// scheme://host:port form Paho expects.
func brokerURL(raw string) (string, error) {
	if !strings.Contains(raw, "://") {
		raw = "tcp://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("bad broker URL %q: %w", raw, err)
	}

	scheme, port := "tcp", "1883"

	switch u.Scheme {
	case "tcp":
	case "mqtts":
		scheme, port = "ssl", "8883"
	default:
		return "", fmt.Errorf("unsupported broker scheme %q (use tcp:// or mqtts://)", u.Scheme)
	}

	if u.Hostname() == "" {
		return "", fmt.Errorf("broker URL %q has no host", raw)
	}

	if p := u.Port(); p != "" {
		port = p
	}

	return fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(u.Hostname(), port)), nil
}

// Dial connects to the broker; every later operation is bounded by the deadline.
func Dial(broker, username, password string, deadline time.Time) (*Client, error) {
	addr, err := brokerURL(broker)
	if err != nil {
		return nil, err
	}

	id := make([]byte, 6)

	_, err = rand.Read(id)
	if err != nil {
		return nil, err
	}

	c := &Client{
		msgs:     make(chan Message, 64),
		done:     make(chan struct{}),
		stop:     make(chan struct{}),
		deadline: deadline,
	}

	opts := paho.NewClientOptions().
		AddBroker(addr).
		SetClientID("hive-" + hex.EncodeToString(id)).
		SetUsername(username).
		SetPassword(password).
		SetCleanSession(true).
		SetAutoReconnect(false).
		SetConnectRetry(false).
		SetTLSConfig(&tls.Config{MinVersion: tls.VersionTLS12}).
		SetConnectionLostHandler(func(_ paho.Client, err error) {
			c.err = err
			close(c.done)
		})

	c.client = paho.NewClient(opts)

	err = c.wait(c.client.Connect())
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MQTT broker %s: %w", addr, err)
	}

	return c, nil
}

// wait blocks until the token completes or the deadline passes.
func (c *Client) wait(token paho.Token) error {
	if !token.WaitTimeout(time.Until(c.deadline)) {
		return ErrTimeout
	}

	return token.Error()
}

// Subscribe subscribes to the filter at QoS 0 and waits for the broker to acknowledge.
func (c *Client) Subscribe(filter string) error {
	err := c.wait(c.client.Subscribe(filter, 0, func(_ paho.Client, m paho.Message) {
		msg := Message{Topic: m.Topic(), Payload: append([]byte(nil), m.Payload()...), Retained: m.Retained()}

		select {
		case c.msgs <- msg:
		case <-c.stop:
		case <-c.done:
		}
	}))
	if err != nil {
		return fmt.Errorf("failed to subscribe to %s (check ACLs): %w", filter, err)
	}

	return nil
}

// Publish publishes at QoS 0 (not retained).
func (c *Client) Publish(topic string, payload []byte) error {
	return c.wait(c.client.Publish(topic, 0, false, payload))
}

// Next returns the next incoming message. It returns ErrQuiet if nothing arrives within wait (wait
// <= 0 means no limit), and ErrTimeout or a connection error once the overall deadline passes or
// the link fails.
func (c *Client) Next(ctx context.Context, wait time.Duration) (Message, error) {
	ctx, cancel := context.WithDeadline(ctx, c.deadline)
	defer cancel()

	var quiet <-chan time.Time

	if wait > 0 {
		t := time.NewTimer(wait)
		defer t.Stop()

		quiet = t.C
	}

	select {
	case m := <-c.msgs:
		return m, nil
	case <-quiet:
		return Message{}, ErrQuiet
	case <-ctx.Done():
		return Message{}, c.ctxErr(ctx)
	case <-c.done:
		return c.lost()
	}
}

// ctxErr maps the deadline expiring to ErrTimeout, leaving real cancellation alone.
func (c *Client) ctxErr(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return ErrTimeout
	}

	return ctx.Err()
}

// lost drains any message received before the connection dropped, then reports the failure.
func (c *Client) lost() (Message, error) {
	select {
	case m := <-c.msgs:
		return m, nil
	default:
	}

	if c.err != nil {
		return Message{}, c.err
	}

	return Message{}, io.EOF
}

// Close disconnects from the broker.
func (c *Client) Close() {
	close(c.stop)
	c.client.Disconnect(250)
}
