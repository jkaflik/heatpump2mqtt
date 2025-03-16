package internal

import (
	"io"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog/log"
)

var goodreads, totalreads int64

// Buffer for storing incomplete data between reads
var persistentBuffer []byte

// Maximum size of the persistent buffer to prevent memory issues
const maxBufferSize = 1024

// isValidReceiveChecksum validates the checksum of received data
func isValidReceiveChecksum(data []byte) bool {
	var chk byte = 0
	for _, v := range data {
		chk += v
	}
	return (chk == 0) // all received bytes + checksum should result in 0
}

// calcChecksum calculates the checksum for a command
func calcChecksum(command []byte) byte {
	var chk byte = 0
	for _, v := range command {
		chk += v
	}
	return (chk ^ 0xFF) + 01
}

// SendCommand sends a command with checksum to the device
func SendCommand(conn DeviceConnection, command []byte) {
	if !conn.IsConnected() {
		log.Error().Msg("Cannot send command - connection is not established")
		if err := conn.Reconnect(); err != nil {
			log.Error().Err(err).Msg("Failed to reconnect device")
			return
		}
	}

	var chk = calcChecksum(command)

	// First send command
	_, err := conn.Write(command)
	if err != nil {
		log.Error().Err(err).Msg("Failed to write command to device")
		return
	}

	// Then send calculated checksum byte
	_, err = conn.Write([]byte{chk})
	if err != nil {
		log.Error().Err(err).Msg("Failed to write checksum to device")
		return
	}

	log.Debug().Hex("checksum", []byte{chk}).Msg("Sent command with checksum")
}

// ReadSerial reads and processes data from the device
func ReadSerial(conn DeviceConnection, mclient mqtt.Client) {
	const (
		maxDataLength = 255
	)

	if !conn.IsConnected() {
		log.Warn().Msg("Cannot read from device - connection is not established")
		return // Connection will be reestablished by the main loop
	}

	// Read new data
	newData := make([]byte, maxDataLength)
	n, err := conn.Read(newData)
	if err != nil {
		if err != io.EOF {
			log.Error().Err(err).Msg("Error reading from device")
		}
		return
	}

	if n == 0 {
		// No data
		return
	}

	// Debug log received data
	if log.Debug().Enabled() {
		log.Debug().Int("bytes", n).Hex("data", newData[:n]).Msg("Read raw data from device")
	}

	// Append new data to persistent buffer
	persistentBuffer = append(persistentBuffer, newData[:n]...)

	// Limit buffer size to prevent memory issues
	if len(persistentBuffer) > maxBufferSize {
		// If buffer grows too large, keep the most recent portion
		persistentBuffer = persistentBuffer[len(persistentBuffer)-maxBufferSize:]
		log.Warn().Int("size", len(persistentBuffer)).Msg("Buffer was truncated to prevent memory issues")
	}

	// Process all complete packets in the buffer
	processPacketsFromBuffer(mclient)
}

// processPacketsFromBuffer processes all complete packets in the persistent buffer
func processPacketsFromBuffer(mclient mqtt.Client) {
	const (
		headerByte   = 113 // 0x71
		secondHeader = 200 // 0xC8
	)

	for {
		// Only process if we have enough data for a minimal packet (header, length, checksum = 3 bytes)
		if len(persistentBuffer) < 3 {
			return
		}

		// Find header in the buffer
		headerPos := -1
		for i := 0; i < len(persistentBuffer)-1; i++ {
			if persistentBuffer[i] == headerByte && persistentBuffer[i+1] == secondHeader {
				headerPos = i
				break
			}
		}

		// No header found, wait for more data
		if headerPos == -1 {
			return
		}

		// If header isn't at the start, trim the buffer
		if headerPos > 0 {
			log.Debug().Int("position", headerPos).Msg("Found header at non-zero position, trimming buffer")
			persistentBuffer = persistentBuffer[headerPos:]
			headerPos = 0
		}

		// Check if we have enough bytes to read packet length
		if len(persistentBuffer) < 3 {
			// Need more data to read length byte
			return
		}

		// Extract the length field
		packetLength := int(persistentBuffer[2])

		// Validate packet length is reasonable
		if packetLength <= 0 || packetLength > 250 {
			log.Debug().Int("length", packetLength).Msg("Invalid packet length, discarding header")
			persistentBuffer = persistentBuffer[2:] // Skip header and try again
			continue
		}

		// Calculate total expected bytes: header (2) + length field (1) + data (packetLength) + checksum (1)
		expectedTotal := packetLength + 3

		// Check if we have a complete packet
		if len(persistentBuffer) < expectedTotal {
			// Incomplete packet, wait for more data
			log.Debug().
				Int("available", len(persistentBuffer)).
				Int("expected", expectedTotal).
				Msg("Waiting for more data to complete packet")
			return
		}

		// Extract the complete packet
		packet := make([]byte, expectedTotal)
		copy(packet, persistentBuffer[:expectedTotal])

		// Validate checksum
		totalreads++
		if !isValidReceiveChecksum(packet) {
			log.Warn().Msg("Checksum validation failed! Skipping header and continuing")
			persistentBuffer = persistentBuffer[2:] // Skip header and try to find another packet
			continue
		}

		// Packet is valid, remove it from buffer
		persistentBuffer = persistentBuffer[expectedTotal:]

		// Update statistics
		goodreads++
		readPercentage := float64(totalreads-goodreads) / float64(totalreads) * 100.0

		log.Info().
			Int64("total", totalreads).
			Int64("errors", totalreads-goodreads).
			Float64("error_percent", readPercentage).
			Msg("Read statistics")

		// Process data based on packet length
		switch expectedTotal {
		case 203:
			// Main datagram
			decodeHeatpumpData(packet, mclient)
		case 20:
			// Optional PCB acknowledge answer
			log.Debug().Msg("Received optional PCB ack answer")
			decodeOptionalHeatpumpData(packet, mclient)
		default:
			log.Debug().Int("length", expectedTotal).Msg("Received a datagram with a non-standard length")
		}

		// Continue processing if there's more data in the buffer
		if len(persistentBuffer) > 0 {
			log.Debug().Int("remaining", len(persistentBuffer)).Msg("More data in buffer")
			continue
		}

		// Buffer is empty, we're done
		return
	}
}
