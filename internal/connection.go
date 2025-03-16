package internal

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"syscall"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/tarm/serial"
)

// DeviceConnection represents a generic connection to a heat pump device
type DeviceConnection interface {
	io.ReadWriteCloser
	IsConnected() bool
	Reconnect() error
}

// SerialConnection represents a serial port connection
type SerialConnection struct {
	port     *serial.Port
	config   *serial.Config
	isActive bool
}

// TCPConnection represents a TCP connection
type TCPConnection struct {
	conn     net.Conn
	address  string
	timeout  time.Duration
	isActive bool
}

// NewDeviceConnection creates the appropriate connection based on configuration
func NewDeviceConnection(config *ConfigStruct) (DeviceConnection, error) {
	switch config.ConnectionType {
	case "tcp":
		// Use the configured timeout or default to 5 seconds
		timeoutDuration := time.Duration(config.TCPTimeout) * time.Second
		return NewTCPConnection(config.Device, timeoutDuration)
	case "serial", "":
		return NewSerialConnection(config.Device, 9600)
	default:
		return nil, fmt.Errorf("unknown connection type: %s", config.ConnectionType)
	}
}

// NewSerialConnection creates a new serial connection
func NewSerialConnection(device string, baudRate int) (*SerialConnection, error) {
	cfg := &serial.Config{
		Name:        device,
		Baud:        baudRate,
		Parity:      serial.ParityEven,
		StopBits:    serial.Stop1,
		ReadTimeout: 2 * time.Second,
	}

	port, err := serial.OpenPort(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to open serial port %s: %w", device, err)
	}

	log.Info().Str("device", device).Int("baudRate", baudRate).Msg("Serial connection established")

	return &SerialConnection{
		port:     port,
		config:   cfg,
		isActive: true,
	}, nil
}

// Read reads from the serial port
func (s *SerialConnection) Read(p []byte) (n int, err error) {
	if !s.isActive {
		return 0, fmt.Errorf("connection is closed")
	}
	return s.port.Read(p)
}

// Write writes to the serial port
func (s *SerialConnection) Write(p []byte) (n int, err error) {
	if !s.isActive {
		return 0, fmt.Errorf("connection is closed")
	}
	return s.port.Write(p)
}

// Close closes the serial port
func (s *SerialConnection) Close() error {
	s.isActive = false
	return s.port.Close()
}

// IsConnected returns the connection state
func (s *SerialConnection) IsConnected() bool {
	return s.isActive
}

// Reconnect attempts to reestablish the serial connection
func (s *SerialConnection) Reconnect() error {
	if s.isActive {
		s.Close()
	}

	port, err := serial.OpenPort(s.config)
	if err != nil {
		return fmt.Errorf("failed to reconnect to serial port %s: %w", s.config.Name, err)
	}

	s.port = port
	s.isActive = true
	log.Info().Str("device", s.config.Name).Msg("Serial connection reestablished")

	return nil
}

// NewTCPConnection creates a new TCP connection
func NewTCPConnection(address string, timeout time.Duration) (*TCPConnection, error) {
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to TCP address %s: %w", address, err)
	}

	// Configure TCP connection
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		// Disable Nagle's algorithm for lower latency
		if err := tcpConn.SetNoDelay(true); err != nil {
			log.Warn().Err(err).Msg("Failed to set TCP_NODELAY")
		}

		// Keep connection alive
		if err := tcpConn.SetKeepAlive(true); err != nil {
			log.Warn().Err(err).Msg("Failed to set keep alive")
		}

		// Set keep alive period
		if err := tcpConn.SetKeepAlivePeriod(30 * time.Second); err != nil {
			log.Warn().Err(err).Msg("Failed to set keep alive period")
		}
	}

	log.Info().Str("address", address).Msg("TCP connection established")

	return &TCPConnection{
		conn:     conn,
		address:  address,
		timeout:  timeout,
		isActive: true,
	}, nil
}

// Read reads from the TCP connection
func (t *TCPConnection) Read(p []byte) (n int, err error) {
	if !t.isActive || t.conn == nil {
		return 0, fmt.Errorf("connection is closed")
	}

	// Set read deadline to prevent hanging
	//if err := t.conn.SetReadDeadline(time.Now().Add(t.timeout)); err != nil {
	//	return 0, fmt.Errorf("failed to set read deadline: %w", err)
	//}

	n, err = t.conn.Read(p)

	// Handle timeout errors specially - we don't want to consider them fatal
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			return n, io.EOF // Return EOF instead of timeout error to prevent connection closing
		}

		if t.isBrokenConnectionErr(err) {
			log.Warn().Msg("Broken connection error occurred, will reconnect on next operation")
			t.isActive = false
		}
	}

	return n, err
}

// Write writes to the TCP connection
func (t *TCPConnection) Write(p []byte) (n int, err error) {
	if !t.isActive || t.conn == nil {
		return 0, fmt.Errorf("connection is closed")
	}

	// Set write deadline to prevent hanging
	if err := t.conn.SetWriteDeadline(time.Now().Add(t.timeout)); err != nil {
		return 0, fmt.Errorf("failed to set write deadline: %w", err)
	}

	// Try to write the data
	n, err = t.conn.Write(p)

	// Handle timeout errors specially - we need to reconnect
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			log.Warn().Msg("Write timeout occurred, will reconnect on next operation")
			t.isActive = false
		}

		if t.isBrokenConnectionErr(err) {
			log.Warn().Msg("Broken connection error occurred, will reconnect on next operation")
			t.isActive = false
		}
	}

	return n, err
}

// isBrokenConnectionErr checks if the error is due to a broken connection
// like a ECONNRESET or EPIPE error
func (t *TCPConnection) isBrokenConnectionErr(err error) bool {
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var sysErr *os.SyscallError
		if errors.As(opErr.Err, &sysErr) {
			switch sysErr.Err {
			case syscall.EPIPE, syscall.ECONNRESET:
				return true
			default:
			}
		}
	}
	return false
}

// Close closes the TCP connection
func (t *TCPConnection) Close() error {
	t.isActive = false
	if t.conn != nil {
		return t.conn.Close()
	}
	return nil
}

// IsConnected returns the connection state
func (t *TCPConnection) IsConnected() bool {
	return t.isActive && t.conn != nil
}

// Reconnect attempts to reestablish the TCP connection
func (t *TCPConnection) Reconnect() error {
	if t.isActive && t.conn != nil {
		t.conn.Close()
	}

	// Try to establish new connection
	conn, err := net.DialTimeout("tcp", t.address, t.timeout)
	if err != nil {
		return fmt.Errorf("failed to reconnect to TCP address %s: %w", t.address, err)
	}

	// Configure TCP connection
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		if err := tcpConn.SetNoDelay(true); err != nil {
			log.Warn().Err(err).Msg("Failed to set TCP_NODELAY on reconnect")
		}

		if err := tcpConn.SetKeepAlive(true); err != nil {
			log.Warn().Err(err).Msg("Failed to set keep alive on reconnect")
		}

		if err := tcpConn.SetKeepAlivePeriod(30 * time.Second); err != nil {
			log.Warn().Err(err).Msg("Failed to set keep alive period on reconnect")
		}
	}

	t.conn = conn
	t.isActive = true
	log.Info().Str("address", t.address).Msg("TCP connection reestablished")

	return nil
}
