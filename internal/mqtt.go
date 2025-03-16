package internal

import (
	"context"
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog/log"
)

// mqttPublish publishes a message to the MQTT broker with enhanced error handling
func mqttPublish(mclient mqtt.Client, topic string, data interface{}, qos byte) {
	token := mclient.Publish(topic, qos, true, data)
	if token.Wait() && token.Error() != nil {
		log.Error().
			Err(token.Error()).
			Str("topic", topic).
			Interface("data", data).
			Msg("Failed to publish MQTT message")
	}
}

// MQTTClient represents a wrapper around the MQTT client with additional functionality
type MQTTClient struct {
	client    mqtt.Client
	cmdChan   chan []byte
	ctx       context.Context
	cancelCtx context.CancelFunc
}

// NewMQTTClient creates and returns a new MQTT client wrapper
func NewMQTTClient(ctx context.Context, commandsChannel chan []byte) *MQTTClient {
	// Create a derived context that can be cancelled
	ctxWithCancel, cancel := context.WithCancel(ctx)

	return &MQTTClient{
		cmdChan:   commandsChannel,
		ctx:       ctxWithCancel,
		cancelCtx: cancel,
	}
}

// Connect establishes a connection to the MQTT broker
func (m *MQTTClient) Connect() error {
	log.Info().Msg("Setting up MQTT connection...")

	// Create client options
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%s", Config.MqttServer, Config.MqttPort))

	// Set credentials if provided
	if Config.MqttLogin != "" {
		opts.SetUsername(Config.MqttLogin)
		opts.SetPassword(Config.MqttPass)
	}

	// Set client ID with unique suffix to avoid conflicts
	clientID := fmt.Sprintf("heatpump2mqtt-%s-%d", Config.DeviceName, time.Now().Unix())
	opts.SetClientID(clientID)

	// Configure last will message for proper availability status
	opts.SetWill(Config.mqttWillTopic, "offline", 0, true)
	opts.SetKeepAlive(time.Duration(Config.MqttKeepalive) * time.Second)

	// Session settings
	opts.SetCleanSession(true) // don't want to receive backlog of setting changes
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetMaxReconnectInterval(1 * time.Minute)

	// Set callback handlers
	opts.SetOnConnectHandler(m.onConnect)
	opts.SetConnectionLostHandler(m.onConnectionLost)

	// Create and connect the client
	m.client = mqtt.NewClient(opts)
	token := m.client.Connect()

	if token.Wait() && token.Error() != nil {
		return fmt.Errorf("failed to connect to MQTT broker: %w", token.Error())
	}

	log.Info().
		Str("broker", Config.MqttServer).
		Str("port", Config.MqttPort).
		Str("clientID", clientID).
		Msg("MQTT connection established")

	return nil
}

// onConnect is called when a connection is established to the broker
func (m *MQTTClient) onConnect(client mqtt.Client) {
	// Publish online status
	mqttPublish(client, Config.mqttWillTopic, "online", 0)

	// Subscribe to command topics if not in listen-only mode
	if !Config.ListenOnly {
		token := client.Subscribe(getCommandTopic("+"), 0, onGenericCommand)
		if token.Wait() && token.Error() != nil {
			log.Error().Err(token.Error()).Msg("Failed to subscribe to command topic")
		}

		token = client.Subscribe(getStatusTopic("+/set"), 0, onAquareaCommand(m.cmdChan))
		if token.Wait() && token.Error() != nil {
			log.Error().Err(token.Error()).Msg("Failed to subscribe to set topic")
		}

		log.Info().Msg("Subscribed to command topics")
	} else {
		log.Info().Msg("Running in listen-only mode, not subscribing to command topics")
	}
}

// onConnectionLost is called when the connection to the broker is lost
func (m *MQTTClient) onConnectionLost(client mqtt.Client, err error) {
	log.Warn().Err(err).Msg("MQTT connection lost, attempting to reconnect")
}

// Disconnect cleanly disconnects from the MQTT broker
func (m *MQTTClient) Disconnect() {
	if m.client != nil && m.client.IsConnected() {
		log.Info().Msg("Disconnecting from MQTT broker")
		m.client.Disconnect(250) // Wait 250ms to complete pending work
	}
	m.cancelCtx() // Cancel the context
}

// Publish publishes a message to a topic
func (m *MQTTClient) Publish(topic string, data interface{}, qos byte) {
	mqttPublish(m.client, topic, data, qos)
}

// Client returns the underlying MQTT client
func (m *MQTTClient) Client() mqtt.Client {
	return m.client
}

// Legacy function for backward compatibility
func MakeMQTTConn(commandsChannel chan []byte) mqtt.Client {
	mqttClient := NewMQTTClient(context.Background(), commandsChannel)
	err := mqttClient.Connect()
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to connect to MQTT broker")
	}
	return mqttClient.Client()
}
