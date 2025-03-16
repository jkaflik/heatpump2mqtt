package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/alexflint/go-arg"
	"github.com/jkaflik/heatpump2mqtt/internal"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"
)

const (
	serialTimeout           = 2 * time.Second
	optionalPCBSaveTime     = 5 * time.Minute
	optionalPCBFile         = "/etc/gh/optionalpcb.raw"
	commandChannelSize      = 10
	gracefulShutdownTimeout = 5 * time.Second
)

var (
	// Panasonic query byte array for main functionality
	panasonicQuery = []byte{
		0x71, 0x6c, 0x01, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}

	// Optional PCB query byte array
	optionalPCBQuery = []byte{
		0xF1, 0x11, 0x01, 0x50, 0x00, 0x00, 0x40, 0xFF, 0xFF, 0xE5, 0xFF, 0xFF, 0x00, 0xFF, 0xEB, 0xFF,
		0xFF, 0x00, 0x00,
	}

	// Command line arguments
	args struct {
		ConfigFile string `arg:"-c,--config" help:"config file path" default:"config.yaml"`
		TopicsFile string `arg:"-t,--topics" help:"topics file path" default:"topics.yaml"`
		Version    bool   `arg:"-v,--version" help:"show version information"`
	}

	// Application version
	version = "1.0.0"
)

// App represents the main application
type App struct {
	deviceConn      internal.DeviceConnection
	commandsChannel chan []byte
	mqttClient      *internal.MQTTClient
	wg              sync.WaitGroup
}

// NewApp creates a new application instance
func NewApp() *App {
	return &App{
		commandsChannel: make(chan []byte, commandChannelSize),
	}
}

// Initialize sets up the application
func (a *App) Initialize(ctx context.Context) error {
	// Load configuration
	internal.Config = internal.ReadConfig(args.ConfigFile)

	// Create device connection (serial or TCP)
	var err error
	a.deviceConn, err = internal.NewDeviceConnection(internal.Config)
	if err != nil {
		return fmt.Errorf("failed to create device connection: %w", err)
	}

	// Load topics
	if err := internal.LoadTopics(args.TopicsFile); err != nil {
		return err
	}

	// Load optional PCB data if enabled
	if internal.Config.OptionalPCB {
		a.loadOptionalPCB()
	}

	// Initialize MQTT client
	a.mqttClient = internal.NewMQTTClient(ctx, a.commandsChannel)
	if err := a.mqttClient.Connect(); err != nil {
		return err
	}

	// Publish Home Assistant discovery topics if enabled
	if internal.Config.HAAutoDiscover {
		internal.PublishDiscoveryTopics(a.mqttClient.Client())
	}

	return nil
}

// Run starts the main application loop
func (a *App) Run(ctx context.Context) error {
	// Create a context group for handling errors
	g, ctx := errgroup.WithContext(ctx)

	// Start the serial processing goroutine
	g.Go(func() error {
		return a.processSerial(ctx)
	})

	// Start the command processing goroutine
	g.Go(func() error {
		return a.processCommands(ctx)
	})

	// Start the optional PCB save goroutine if enabled
	if internal.Config.OptionalPCB {
		g.Go(func() error {
			return a.saveOptionalPCBPeriodically(ctx)
		})
	}

	// Send initial query
	internal.SendCommand(a.deviceConn, panasonicQuery)

	// Wait for all goroutines to complete or context to be cancelled
	return g.Wait()
}

// processSerial handles the serial port communication
func (a *App) processSerial(ctx context.Context) error {
	ticker := time.NewTicker(serialTimeout)
	defer ticker.Stop()

	// Track consecutive errors for reconnection
	consecutiveErrors := 0

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			// Check if connection needs to be reestablished
			if !a.deviceConn.IsConnected() {
				log.Warn().Msg("Device connection lost, attempting to reconnect")
				if err := a.deviceConn.Reconnect(); err != nil {
					log.Error().Err(err).Msg("Failed to reconnect device")
					consecutiveErrors++

					// If we've failed multiple times, wait longer before retrying
					if consecutiveErrors > 3 {
						time.Sleep(5 * time.Second)
					}
					continue
				}
				consecutiveErrors = 0
			}

			// Read from device connection
			internal.ReadSerial(a.deviceConn, a.mqttClient.Client())

			// Reset error counter on successful read
			consecutiveErrors = 0

			// Log command queue length if it's getting large
			queueLen := len(a.commandsChannel)
			if queueLen > 10 {
				log.Warn().Int("queueLength", queueLen).Msg("Command queue is building up")
			}
		}
	}
}

// processCommands handles command scheduling and execution
func (a *App) processCommands(ctx context.Context) error {
	queryTicker := time.NewTicker(time.Second * time.Duration(internal.Config.ReadInterval))
	defer queryTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case value := <-a.commandsChannel:
			// Process incoming command
			internal.SendCommand(a.deviceConn, value)

		case <-queryTicker.C:
			// Schedule regular query
			select {
			case a.commandsChannel <- panasonicQuery:
				// Successfully scheduled
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(500 * time.Millisecond):
				log.Warn().Msg("Command channel full, couldn't schedule regular query")
			}

			// Also send optional PCB query if enabled
			if internal.Config.OptionalPCB && !internal.Config.ListenOnly {
				select {
				case a.commandsChannel <- optionalPCBQuery:
					// Successfully scheduled
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(500 * time.Millisecond):
					log.Warn().Msg("Command channel full, couldn't schedule PCB query")
				}
			}
		}
	}
}

// saveOptionalPCBPeriodically periodically saves the optional PCB data
func (a *App) saveOptionalPCBPeriodically(ctx context.Context) error {
	ticker := time.NewTicker(optionalPCBSaveTime)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			a.saveOptionalPCB()
		}
	}
}

// saveOptionalPCB writes the optional PCB data to file
func (a *App) saveOptionalPCB() {
	err := os.WriteFile(optionalPCBFile, optionalPCBQuery, 0644)
	if err != nil {
		log.Error().Err(err).Str("file", optionalPCBFile).Msg("Failed to save optional PCB data")
	} else {
		log.Debug().Str("file", optionalPCBFile).Msg("Optional PCB data stored")
	}
}

// loadOptionalPCB reads the optional PCB data from file
func (a *App) loadOptionalPCB() {
	data, err := os.ReadFile(optionalPCBFile)
	if err != nil {
		log.Warn().Err(err).Str("file", optionalPCBFile).Msg("Failed to load optional PCB data")
	} else {
		optionalPCBQuery = data
		log.Info().Str("file", optionalPCBFile).Msg("Optional PCB data loaded")
	}
}

// Shutdown performs a graceful shutdown of the application
func (a *App) Shutdown() {
	log.Info().Msg("Shutting down...")

	// Save PCB data one final time if enabled
	if internal.Config.OptionalPCB {
		a.saveOptionalPCB()
	}

	// Close device connection
	if a.deviceConn != nil {
		a.deviceConn.Close()
		log.Debug().Msg("Device connection closed")
	}

	// Disconnect MQTT
	if a.mqttClient != nil {
		a.mqttClient.Disconnect()
		log.Debug().Msg("MQTT client disconnected")
	}

	log.Info().Msg("Shutdown complete")
}

func main() {
	// Parse command line arguments
	arg.MustParse(&args)

	// Show version if requested
	if args.Version {
		println(version)
		return
	}

	// Create application instance
	app := NewApp()

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle termination signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		log.Info().Str("signal", sig.String()).Msg("Received termination signal")
		cancel()

		// Force shutdown after timeout
		select {
		case <-time.After(gracefulShutdownTimeout):
			log.Warn().Msg("Graceful shutdown timed out, forcing exit")
			os.Exit(1)
		case <-sigCh:
			// Second signal, force immediate exit
			log.Warn().Msg("Received second termination signal, forcing exit")
			os.Exit(1)
		}
	}()

	// Initialize the application
	if err := app.Initialize(ctx); err != nil {
		log.Fatal().Err(err).Msg("Failed to initialize application")
	}

	// Start the application
	log.Info().
		Str("version", version).
		Str("device", internal.Config.Device).
		Str("connectionType", internal.Config.ConnectionType).
		Int("readInterval", internal.Config.ReadInterval).
		Msg("heatpump2mqtt starting...")

	if err := app.Run(ctx); err != nil && err != context.Canceled {
		log.Error().Err(err).Msg("Application error")
	}

	// Perform graceful shutdown
	app.Shutdown()
}
