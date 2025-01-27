package internal

import (
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/tarm/serial"
	"io"
	"log"
)

var goodreads, totalreads int64

func isValidReceiveChecksum(data []byte) bool {
	var chk byte = 0
	for _, v := range data {
		chk += v
	}
	return (chk == 0) //all received bytes + checksum should result in 0
}

func calcChecksum(command []byte) byte {
	var chk byte = 0
	for _, v := range command {
		chk += v
	}
	return (chk ^ 0xFF) + 01
}

func SendCommand(serialPort *serial.Port, command []byte) {
	var chk = calcChecksum(command)

	_, err := serialPort.Write(command) //first send command
	if err != nil {
		log.Println(err)
	}
	_, err = serialPort.Write([]byte{chk}) //then calculcated checksum byte afterwards
	if err != nil {
		log.Println(err)
	}
	LogHex("command", command)
}

func ReadSerial(serialPort *serial.Port, mclient mqtt.Client) {
	const maxDataLength = 255

	data := make([]byte, maxDataLength)
	n, err := serialPort.Read(data)
	if err != nil {
		if err != io.EOF {
			log.Println(err)
		}
		return
	}
	if n == 0 {
		//no data
		return
	}
	totalreads++

	if data[0] != 113 { //wrong header received!
		log.Println("Received bad header. Ignoring this data!")
		LogHex("bad header", data[:n])
		return
	}

	if n > 1 { //should have received length part of header now

		if (n > int(data[1]+3)) || (n >= maxDataLength) {
			log.Println("Received more data than header suggests! Ignoring this as this is bad data.")
			LogHex("bad data", data[:n])
			return
		}

		if n == int(data[1]+3) { //we received all data (data[1] is header length field)
			log.Printf("Received %d bytes data", n)
			LogHex("data", data[:n])

			if !isValidReceiveChecksum(data[:n]) {
				log.Println("Checksum received false!")
				return
			}
			goodreads++
			readpercentage := float64(totalreads-goodreads) / float64(totalreads) * 100.
			log.Println(fmt.Sprintf("RX: %d RX errors: %d (%.2f %%)", totalreads, totalreads-goodreads, readpercentage))

			if n == 203 { //for now only return true for this datagram because we can not decode the shorter datagram yet
				decodeHeatpumpData(data[:n], mclient)
			} else if n == 20 { //optional pcb acknowledge answer
				log.Println("Received optional PCB ack answer. Decoding this in OPT topics.")
				decodeOptionalHeatpumpData(data[:n], mclient)
			} else {
				log.Println("Received a shorter datagram. Can't decode this yet.")
			}
		}
	}
}
