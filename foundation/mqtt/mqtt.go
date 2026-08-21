package mqtt

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const topicBufferSize = 500
const keepAlive = 30 * time.Second
const pingTimeout = 5 * time.Second

type Configs struct {
	Host         string
	Port         int
	Username     string
	Password     string
	ClientID     string
	CleanSession bool

	LastWillTopic   string
	LastWillPayload []byte
	RetainLastWill  bool
}

type subscription struct {
	ch  chan []byte
	qos byte
}

type Client struct {
	client mqtt.Client

	mu        sync.Mutex
	topics    map[string]subscription
	onConnect func()
}

func (c *Client) SetOnConnect(f func()) {
	c.mu.Lock()
	c.onConnect = f
	c.mu.Unlock()
}

func (c *Client) Publish(topic string, payload []byte, qos byte, retained bool) error {
	token := c.client.Publish(topic, qos, retained, payload)
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

func (c *Client) handler() func(mqtt.Client, mqtt.Message) {
	return func(_ mqtt.Client, m mqtt.Message) {
		c.mu.Lock()
		sub, ok := c.topics[m.Topic()]
		if !ok {
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
		sub.ch <- m.Payload()
	}
}

func (c *Client) Subscribe(topic string, qos byte) (chan []byte, error) {
	c.mu.Lock()
	sub, ok := c.topics[topic]
	if !ok {
		sub = subscription{ch: make(chan []byte, topicBufferSize), qos: qos}
		c.topics[topic] = sub
	}
	c.mu.Unlock()

	token := c.client.Subscribe(topic, qos, c.handler())
	if token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}
	return sub.ch, nil
}

func (c *Client) Connect() error {
	token := c.client.Connect()
	if token.Wait() && token.Error() != nil {
		return token.Error()
	}
	return nil
}

func (c *Client) Disconnect() {
	c.client.Disconnect(250)
	// buffers intentionally not closed as blocked handler send would panic
	// consumers exit via ctx
}

func New(configs Configs) (*Client, error) {
	c := &Client{topics: make(map[string]subscription)}

	opts := mqtt.NewClientOptions()

	opts.CleanSession = configs.CleanSession
	opts.ClientID = configs.ClientID
	opts.SetKeepAlive(keepAlive)
	opts.SetPingTimeout(pingTimeout)

	if configs.Host == "" {
		return nil, ErrMissingHost
	}
	if configs.Port == 0 {
		return nil, ErrMissingPort
	}
	broker := fmt.Sprintf("tcp://%s:%d", configs.Host, configs.Port)
	opts.AddBroker(broker)

	opts.Username = configs.Username
	opts.Password = configs.Password

	opts.OnConnect = func(_ mqtt.Client) {
		slog.Info("Connected to master mqtt client")
		c.mu.Lock()
		subs := make(map[string]byte, len(c.topics))
		for topic, sub := range c.topics {
			subs[topic] = sub.qos
		}
		c.mu.Unlock()
		for topic, qos := range subs {
			token := c.client.Subscribe(topic, qos, c.handler())
			if token.Wait() && token.Error() != nil {
				slog.Error("mqtt resubscribe failed", "topic", topic)
			}
		}

		c.mu.Lock()
		f := c.onConnect
		c.mu.Unlock()
		if f != nil {
			f()
		}
	}

	opts.OnConnectionLost = func(_ mqtt.Client, err error) {
		slog.Warn("mqtt connection lost", "err", err)
	}

	opts.SetBinaryWill(configs.LastWillTopic, configs.LastWillPayload, 1, configs.RetainLastWill)

	c.client = mqtt.NewClient(opts)
	return c, nil
}
