package internal

import (
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// MQTTLogWriter is a writer that sends logs to MQTT
type MQTTLogWriter struct {
	client mqtt.Client
	topic  string
}

// Write implements the io.Writer interface to enable MQTT logging
func (m *MQTTLogWriter) Write(p []byte) (n int, err error) {
	if m.client == nil || !m.client.IsConnected() {
		return 0, fmt.Errorf("MQTT client not connected")
	}

	// Extract and format the log entry for MQTT
	msg := string(p)
	mqttPublish(m.client, m.topic, msg, 0)
	return len(p), nil
}
