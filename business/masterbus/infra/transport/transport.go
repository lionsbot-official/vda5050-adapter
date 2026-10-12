package transport

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/lionsbot-official/vda5050-adapter/foundation/env"
	"github.com/lionsbot-official/vda5050-adapter/foundation/mqtt"
)

type Transport struct {
	client *mqtt.Client
}

func New(configs env.Configs, serialNumber, willTopic string, willPayload []byte) (*Transport, error) {
	mqttClient, err := mqtt.New(mqtt.Configs{
		Host:         configs.MasterConfig.Host,
		Port:         configs.MasterConfig.Port,
		Username:     configs.MasterConfig.Username,
		Password:     configs.MasterConfig.Password,
		ClientID:     deriveClientID(configs.MasterConfig.ClientID, serialNumber),
		CleanSession: configs.MasterConfig.CleanSession,

		LastWillTopic:   willTopic,
		LastWillPayload: willPayload,
		RetainLastWill:  true,
	})
	if err != nil {
		return nil, err
	}

	return &Transport{
		client: mqttClient,
	}, nil
}

func deriveClientID(base, serialNumber string) string {
	if base == "" {
		return serialNumber
	}
	return base + "-" + serialNumber
}

func (t *Transport) SetOnConnect(f func()) {
	t.client.SetOnConnect(f)
}

func (t *Transport) Connect() error {
	if err := t.client.Connect(); err != nil {
		return fmt.Errorf("transport: error connecting to master mqtt client: %w", err)
	}
	return nil
}

func (t *Transport) Disconnect() {
	t.client.Disconnect()
	slog.Info("transport: disconnected from master mqtt client")
}

func (t *Transport) Publish(topic string, payload any, qos byte, retained bool) error {
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("transport: error marshalling payload: %w", err)
	}
	if err := t.client.Publish(topic, jsonPayload, qos, retained); err != nil {
		return fmt.Errorf("transport: unable to publish to topic %s: %w", topic, err)
	}
	return nil
}

func (t *Transport) Subscribe(topic string, qos byte) (chan []byte, error) {
	ch, err := t.client.Subscribe(topic, qos)
	if err != nil {
		return nil, fmt.Errorf("transport: error subscribing to topic %s: %w", topic, err)
	}
	return ch, nil
}
