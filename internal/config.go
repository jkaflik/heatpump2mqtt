package internal

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-playground/validator/v10"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
)

var Config *ConfigStruct

// ConfigStruct holds the application configuration
type ConfigStruct struct {
	DeviceName      string `mapstructure:"deviceName" validate:"required"`                       // for HA discovery
	Device          string `mapstructure:"device" validate:"required"`                           // serial port or tcp address
	ConnectionType  string `mapstructure:"connectionType" validate:"omitempty,oneof=serial tcp"` // Connection type: 'serial' or 'tcp'
	TCPTimeout      int    `mapstructure:"tcpTimeout" validate:"omitempty,min=1,max=60"`         // TCP connection timeout in seconds
	ReadInterval    int    `mapstructure:"readInterval" validate:"required,min=1,max=60"`        // HP query interval
	ListenOnly      bool   `mapstructure:"listenOnly"`                                           // no commands at all
	OptionalPCB     bool   `mapstructure:"optionalPCB"`                                          // enable optional PCB emulation
	EnableOSCommand bool   `mapstructure:"enableOSCommand"`                                      // enable OS commands

	MqttServer     string `mapstructure:"mqttServer" validate:"required"`
	MqttPort       string `mapstructure:"mqttPort" validate:"required"`
	MqttLogin      string `mapstructure:"mqttLogin"`
	MqttPass       string `mapstructure:"mqttPass"`
	MqttKeepalive  int    `mapstructure:"mqttKeepalive" validate:"required,min=10,max=300"`
	MqttTopicBase  string `mapstructure:"mqttTopicBase" validate:"required"`
	HAAutoDiscover bool   `mapstructure:"haAutoDiscover"`

	LogLevel string `mapstructure:"logLevel" validate:"omitempty,oneof=trace debug info warn error"`

	// topics
	mqttWillTopic      string
	mqttLogTopic       string
	mqttValuesTopic    string
	mqttPcbValuesTopic string
	mqttCommandsTopic  string
}

// InitLogger creates a structured logger with the specified log level
func InitLogger(logLevel string) {
	// Set console writer
	consoleWriter := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}

	// Create multi-writer if log file is needed
	writers := []io.Writer{consoleWriter}

	// Set log level
	level := zerolog.InfoLevel // Default to info
	if logLevel != "" {
		switch strings.ToLower(logLevel) {
		case "trace":
			level = zerolog.TraceLevel
		case "debug":
			level = zerolog.DebugLevel
		case "info":
			level = zerolog.InfoLevel
		case "warn":
			level = zerolog.WarnLevel
		case "error":
			level = zerolog.ErrorLevel
		}
	}

	// Configure the global logger
	zerolog.SetGlobalLevel(level)
	zerolog.TimeFieldFormat = time.RFC3339
	log.Logger = zerolog.New(zerolog.MultiLevelWriter(writers...)).
		With().
		Timestamp().
		Caller().
		Logger()
}

// Topic helper functions
func getStatusTopic(name string) string {
	return fmt.Sprintf("%s/%s", Config.mqttValuesTopic, name)
}

func getCommandTopic(name string) string {
	return fmt.Sprintf("%s/%s", Config.mqttCommandsTopic, name)
}

func getPcbStatusTopic(name string) string {
	return fmt.Sprintf("%s/%s", Config.mqttPcbValuesTopic, name)
}

// LogErrorPause logs an error and pauses execution
func LogErrorPause(err error) {
	log.Error().Err(err).Msg("Fatal error, cannot continue")
	log.Error().Msg("Awaiting new Config")
	for {
		time.Sleep(10 * time.Second)
	}
}

// ReadConfig loads application configuration from file or environment variables
func ReadConfig(configFile string) *ConfigStruct {
	// Initialize logger with default level, will be updated later
	InitLogger("info")

	v := viper.New()

	// Set default values
	v.SetDefault("readInterval", 10)
	v.SetDefault("connectionType", "serial")
	v.SetDefault("tcpTimeout", 5) // 5 seconds TCP timeout
	v.SetDefault("listenOnly", false)
	v.SetDefault("optionalPCB", false)
	v.SetDefault("enableOSCommand", true)
	v.SetDefault("mqttServer", "127.0.0.1")
	v.SetDefault("mqttPort", "1883")
	v.SetDefault("mqttKeepalive", 60)
	v.SetDefault("mqttTopicBase", "panasonic_heat_pump")
	v.SetDefault("haAutoDiscover", true)
	v.SetDefault("logLevel", "info")
	v.SetDefault("logMqtt", true)
	v.SetDefault("loghex", false)

	// Configure environment variables
	v.SetEnvPrefix("HEATPUMP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Load from file
	v.SetConfigFile(configFile)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Config file not found, continue with defaults and env vars
			log.Warn().Str("configFile", configFile).Msg("Config file not found, using defaults and environment variables")
		} else {
			// Config file was found but another error was produced
			log.Fatal().Err(err).Str("configFile", configFile).Msg("Error reading config file")
		}
	}

	// Unmarshal config
	var config ConfigStruct
	if err := v.Unmarshal(&config); err != nil {
		log.Fatal().Err(err).Msg("Unable to decode config")
	}

	// Set derived fields
	config.mqttWillTopic = config.MqttTopicBase + "/LWT"
	config.mqttLogTopic = config.MqttTopicBase + "/log"
	config.mqttValuesTopic = config.MqttTopicBase + "/main"
	config.mqttPcbValuesTopic = config.MqttTopicBase + "/optional"
	config.mqttCommandsTopic = config.MqttTopicBase + "/commands"

	// Re-initialize the logger with the config level
	InitLogger(config.LogLevel)

	// Validate config
	validate := validator.New()
	if err := validate.Struct(&config); err != nil {
		log.Fatal().Err(err).Msg("Configuration validation failed")
	}

	log.Info().Msg("Configuration loaded successfully")

	Config = &config
	return Config
}

// LookForSerialPorts attempts to find available serial ports on the system
func LookForSerialPorts() []string {
	// Common paths for USB serial devices
	paths := []string{
		"/dev/ttyUSB*",
		"/dev/ttyACM*",
		"/dev/tty.usbserial*",
		"/dev/tty.usbmodem*",
	}

	var foundPorts []string
	for _, pattern := range paths {
		matches, err := filepath.Glob(pattern)
		if err == nil {
			foundPorts = append(foundPorts, matches...)
		}
	}

	return foundPorts
}
