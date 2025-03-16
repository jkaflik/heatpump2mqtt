# heatpump2mqtt

A Go-based Panasonic heat pump integration for MQTT, with enhanced Home Assistant auto-discovery.

## Features

- Real-time data from Panasonic Aquarea heat pumps to MQTT
- Automatic Home Assistant entity discovery
- Support for both Serial and TCP connections
- Secure and efficient implementation in Go
- Support for energy monitoring and control
- Support for multiple platforms (amd64, arm64)
- Containerized deployment via Docker
- Kubernetes deployment via Helm chart

## Installation

### Using Docker (recommended)

```bash
docker pull ghcr.io/jkaflik/heatpump2mqtt:latest
```

#### With Serial Connection

Run the container with access to your serial device:

```bash
docker run -d \
  --name heatpump2mqtt \
  --restart unless-stopped \
  -v /path/to/config:/config \
  --device=/dev/ttyUSB0:/dev/ttyUSB0 \
  -e HEATPUMP_CONNECTIONTYPE=serial \
  -e HEATPUMP_DEVICE=/dev/ttyUSB0 \
  -e HEATPUMP_MQTTSERVER=mqtt.example.com \
  -e HEATPUMP_MQTTPORT=1883 \
  ghcr.io/jkaflik/heatpump2mqtt:latest
```

#### With TCP Connection

To use a TCP connection (useful when the heat pump is connected to another machine):

```bash
docker run -d \
  --name heatpump2mqtt \
  --restart unless-stopped \
  -v /path/to/config:/config \
  -e HEATPUMP_CONNECTIONTYPE=tcp \
  -e HEATPUMP_DEVICE=192.168.1.100:8899 \
  -e HEATPUMP_MQTTSERVER=mqtt.example.com \
  -e HEATPUMP_MQTTPORT=1883 \
  ghcr.io/jkaflik/heatpump2mqtt:latest
```

### Kubernetes with Helm

Deploy to Kubernetes using the provided Helm chart:

```bash
helm install my-heatpump ./chart \
  --set mqtt.server=mqtt.example.com \
  --set heatpump.device=192.168.1.100:8899 \
  --set heatpump.connectionType=tcp
```

See the [Helm chart README](./chart/README.md) for detailed configuration options.

### Building from source

Requirements:
- Go 1.24 or higher

```bash
git clone https://github.com/jkaflik/heatpump2mqtt.git
cd heatpump2mqtt
go build -o heatpump2mqtt ./cmd/heatpump2mqtt
```

## Configuration

Copy the example configuration:

```bash
cp config.example.yaml config.yaml
```

Edit the configuration file with your settings:

```yaml
# Heat pump device configuration
deviceName: "YourDeviceID"               # Identifier for Home Assistant discovery
device: "/dev/ttyUSB0"                   # Serial port or TCP address (e.g. "192.168.1.100:8899")
connectionType: "serial"                 # Connection type: "serial" or "tcp"
readInterval: 10                         # Seconds between heat pump queries
listenOnly: false                        # If true, won't send any commands
optionalPCB: false                       # Enable optional PCB emulation
enableOSCommand: true                    # Enable OS command execution via MQTT

# MQTT broker settings
mqttServer: "mqtt.example.com"           # MQTT broker hostname or IP
mqttPort: "1883"                         # MQTT broker port
mqttLogin: "user"                        # MQTT username
mqttPass: "password"                     # MQTT password
mqttKeepalive: 60                        # MQTT keepalive interval in seconds
mqttTopicBase: "panasonic_heat_pump"     # Base topic for all MQTT messages
haAutoDiscover: true                     # Enable Home Assistant MQTT discovery

# Logging configuration
logLevel: "info"                         # Log level: trace, debug, info, warn, error
logmqtt: true                            # Log MQTT messages to broker
loghex: false                            # Log hex dumps of all data
```

## Serial to TCP Bridge

For setups where the heat pump is connected to a different machine than the one running heatpump2mqtt, you can use the provided systemd service to create a TCP bridge to the serial port.

See the [examples directory](./examples/README.md) for setup instructions.

## Home Assistant Integration

The application automatically creates Home Assistant MQTT entities for all detected heat pump sensors and controls. Just make sure that:

1. Home Assistant has MQTT integration configured
2. `haAutoDiscover: true` is set in your config.yaml

## Architecture Options

The application supports multiple deployment architectures:

### Direct Serial Connection
```
┌─────────────────┐     Serial     ┌─────────────┐
│  heatpump2mqtt  │◄───Connection──►│ Heat Pump   │
│    Container    │                 │             │
└─────────────────┘                 └─────────────┘
```

### TCP Bridge Connection
```
┌─────────────────┐     TCP      ┌─────────────┐    Serial    ┌─────────────┐
│  heatpump2mqtt  │◄───Connection─►│ TCP Bridge │◄──Connection─►│ Heat Pump   │
│    Container    │               │  Service    │              │             │
└─────────────────┘               └─────────────┘              └─────────────┘
```

### Kubernetes Deployment
```
┌─────────────────────────────────────────────────┐
│                Kubernetes Cluster                │
│                                                 │
│  ┌─────────────────────────────────────────┐    │
│  │           heatpump2mqtt Pod             │    │
│  │                                         │    │
│  │  ┌─────────────────┐                    │    │     ┌─────────────┐
│  │  │                 │    TCP or Serial   │    │     │             │
│  │  │  heatpump2mqtt  │◄───Connection─────────────────► Heat Pump   │
│  │  │    Container    │                    │    │     │             │
│  │  └─────────────────┘                    │    │     └─────────────┘
│  │                                         │    │
│  └─────────────────────────────────────────┘    │
│                                                 │
└─────────────────────────────────────────────────┘
```
