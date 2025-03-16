package internal

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog/log"
)

// onGenericCommand handles generic MQTT commands
func onGenericCommand(_ mqtt.Client, msg mqtt.Message) {
	topicPieces := strings.Split(msg.Topic(), "/")
	function := topicPieces[len(topicPieces)-1]
	value := string(msg.Payload())

	log.Info().
		Str("function", function).
		Str("value", value).
		Str("topic", msg.Topic()).
		Msg("Command received")

	switch {
	case Config.OptionalPCB:
		handlePCBCommand(function, value)
	default:
		log.Warn().Str("command", function).Msg("Unknown command received")
	}
}

// onAquareaCommand returns a handler for Aquarea-specific commands
func onAquareaCommand(commandsChannel chan []byte) func(mclient mqtt.Client, msg mqtt.Message) {
	return func(mclient mqtt.Client, msg mqtt.Message) {
		topicPieces := strings.Split(msg.Topic(), "/")
		function := topicPieces[len(topicPieces)-2]
		value := string(msg.Payload())

		log.Info().
			Str("function", function).
			Str("value", value).
			Str("topic", msg.Topic()).
			Msg("Aquarea command received")

		command, err := prepMainCommand(function, value)
		if err == nil {
			// Send command to the channel with timeout protection
			select {
			case commandsChannel <- command[:]:
				log.Debug().
					Str("function", function).
					Str("value", value).
					Msg("Command sent to channel")
			case <-time.After(2 * time.Second):
				log.Error().
					Str("function", function).
					Str("value", value).
					Msg("Timeout sending command to channel - channel might be full")
			}
		} else {
			log.Error().
				Err(err).
				Str("function", function).
				Str("value", value).
				Msg("Failed to prepare command")
		}
	}
}

// verboseToNumber converts a string value to its numeric equivalent based on the topic's defined values
func verboseToNumber(function, value string) (int64, error) {
	if sensor, ok := topicNameLookup[function]; ok {
		for valueKey, valueName := range sensor.Values {
			if value == valueName {
				return int64(valueKey), nil
			}
		}
	}
	return 0, fmt.Errorf("cannot convert literal '%s' to number for function '%s'", value, function)
}

// prepMainCommand prepares a command byte array for the heat pump
func prepMainCommand(function, msg string) ([setCmdLen]byte, error) {
	command := panasonicSetCommand

	// Try to parse as a number first
	v, err := strconv.ParseInt(msg, 10, 16)
	if err != nil {
		// If not a number, try to convert from a named value
		v, err = verboseToNumber(function, msg)
		if err != nil {
			return command, fmt.Errorf("value conversion error: %w", err)
		}
	}

	// Find the topic and encode the value
	if sensor, ok := topicNameLookup[function]; ok {
		if sensor.EncodeFunction != "" {
			if handler, ok := encodeInt[sensor.EncodeFunction]; ok {
				data := handler(int(v))

				log.Debug().
					Int("offset", sensor.DecodeOffset).
					Int("value", int(data)).
					Str("function", function).
					Str("original", msg).
					Msg("Setting value")

				command[sensor.DecodeOffset] = data
				return command, nil
			}
			return command, fmt.Errorf("unknown encode function '%s' for topic '%s'", sensor.EncodeFunction, function)
		}
		return command, fmt.Errorf("no encode function defined for topic '%s'", function)
	}
	return command, fmt.Errorf("unknown topic '%s'", function)
}

// handlePCBCommand processes commands for the optional PCB
func handlePCBCommand(function, value string) {
	// Handle float commands
	if handler, ok := optionCommandMapFloat[function]; ok {
		temp, err := strconv.ParseFloat(value, 64)
		if err != nil {
			log.Error().
				Err(err).
				Str("function", function).
				Str("value", value).
				Msg("Value conversion error for float command")
			return
		}
		handler(temp)
		log.Debug().
			Str("function", function).
			Float64("value", temp).
			Msg("PCB float command processed")
		return
	}

	// Handle byte commands
	if handler, ok := optionCommandMapByte[function]; ok {
		v, err := strconv.ParseInt(value, 10, 8)
		if err != nil {
			log.Error().
				Err(err).
				Str("function", function).
				Str("value", value).
				Msg("Value conversion error for byte command")
			return
		}
		handler(byte(v))
		log.Debug().
			Str("function", function).
			Int64("value", v).
			Msg("PCB byte command processed")
		return
	}

	// Unknown command
	log.Error().
		Str("function", function).
		Str("value", value).
		Msg("Unknown PCB command or value conversion error")
}
