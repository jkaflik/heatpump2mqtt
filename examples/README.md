# heatpump2mqtt Examples

This directory contains example configurations and utilities for use with the heatpump2mqtt project.

## Serial to TCP Bridge Options

There are several tools you can use to create a TCP bridge to a serial device. 
This is useful when:

1. Your heat pump is connected to a different machine than where you want to run heatpump2mqtt
2. You want to have multiple connections to the heat pump (for debugging or monitoring)
3. You're running heatpump2mqtt in a container without direct access to serial devices

### Installation Prerequisites

Choose one of the following tools to install:

#### ser2net

```bash
# On Debian/Ubuntu
sudo apt-get update
sudo apt-get install ser2net

# On RHEL/CentOS/Fedora
sudo dnf install ser2net

# On OpenSUSE
sudo zypper install ser2net
```

### Installation

1. Make sure your user has access to the serial device:
   ```bash
   # Create a dialout group if it doesn't exist
   sudo groupadd -f dialout
   
   # Add your user to the dialout group
   sudo usermod -a -G dialout $USER
   
   # Set permissions on the serial device
   sudo chmod 666 /dev/ttyUSB0
   ```

2. Copy the service file to the systemd directory:
   ```bash
   sudo cp ser2net.service /etc/systemd/system/
   ```

3. Edit the service file to update the serial port if needed.

4. Enable and start the service:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable ser2net.service
   sudo systemctl start ser2net.service
   ```

5. Check the status:
   ```bash
   sudo systemctl status ser2net.service
   ```

6. Verify that the service is listening on the expected port:
   ```bash
   sudo ss -tuln | grep 8899
   ```

### Testing the Connection

### Troubleshooting

#### Viewing Service Logs

To see the logs from the service:
```bash
sudo journalctl -u ser2net.service -f
```

#### Common Issues and Solutions

1. **Service starts but then stops immediately**
   - Check if the serial port exists: `ls -l /dev/ttyUSB0`
   - Verify permissions: `ls -l /dev/ttyUSB0` should show readable/writable permissions
   - Try running the command manually to see direct output:
   
     ```bash
     sudo ser2net -d -C "8899:raw:0:/dev/ttyUSB0:9600 8DATABITS EVEN 1STOPBIT local"
     ```

2. **Cannot connect to the TCP port**
   - Check if the service is running: `systemctl status heatpump2tcp.service`
   - Verify the port is listening: `ss -tuln | grep 8899`
   - Check firewall settings: `sudo ufw status` or `sudo firewall-cmd --list-all`

3. **Connection works but data times out (i/o timeout errors)**
   - If you see "read tcp x.x.x.x:xxxxx->x.x.x.x:8899: i/o timeout" errors:
     - Remove the `nonblock=1` parameter from socat as it can cause premature timeouts
     - Use `raw` instead of `rawer` for better compatibility with the heat pump
     - Add `nodelay` to TCP-LISTEN to reduce TCP buffering
     - Add `crtscts=0` to disable hardware flow control
     - Add `icanon=0` to disable canonical mode processing
     - Consider adding `so-keepalive=on` to the TCP-LISTEN parameters to maintain the connection
     - If using a firewall or network device in between, ensure it's not timing out idle connections

4. **Connection works but data is corrupted or you only get good data on first read**
   - If you see correct data only on first read, followed by garbage data:
     - Try switching to ser2net which handles binary data better than socat
     - If you must use socat, try these fixes:
       - Use `OPEN` instead of `FILE` in socat (OPEN has better handling of binary data)
       - Make sure parity is explicitly enabled with `parenb=1` 
       - Add `echo=0` to disable local echo
       - Add `igncr=0` and `icrnl=0` to prevent CR/LF translation
       - Move the `fork` option to the end of the TCP-LISTEN options list
     - Try removing and reinserting the USB adapter
   - The Panasonic Aquarea typically uses 9600 baud, 8 data bits, even parity, 1 stop bit.

5. **Multiple devices or changing USB port assignments**
   - Create stable device names using udev rules
   - Identify your device by running: `udevadm info -a -n /dev/ttyUSB0 | grep -E "idVendor|idProduct|serial|manufacturer"`
   - Create a udev rule like:
     ```
     SUBSYSTEM=="tty", ATTRS{idVendor}=="0403", ATTRS{idProduct}=="6001", SYMLINK+="ttyUSB_HEATPUMP", MODE="0666", GROUP="dialout"
     ```
