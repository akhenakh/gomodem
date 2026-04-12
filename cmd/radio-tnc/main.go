package main

import (
	"fmt"
	"log"

	"github.com/akhenakh/gomodem/modem"
	"github.com/akhenakh/gomodem/protocols"
	"github.com/langhuihui/gomem"
	"github.com/songgao/water"
)

const PID_IPv4 byte = 0xCC

func main() {
	// 1. Create the Host OS TUN Interface
	config := water.Config{
		DeviceType: water.TUN,
	}
	// Note: Creating TUN interfaces requires root privileges (sudo)
	iface, err := water.New(config)
	if err != nil {
		log.Fatalf("Failed to create TUN device: %v", err)
	}

	fmt.Printf("Virtual Interface Created: %s\n", iface.Name())
	fmt.Printf("--> Run: sudo ip addr add 10.0.0.1/24 dev %s\n", iface.Name())
	fmt.Printf("--> Run: sudo ip link set dev %s mtu 256 up\n", iface.Name())
	fmt.Printf("--> Run: sudo ip route add 44.0.0.0/8 dev %s\n", iface.Name())
	fmt.Println("Listening for Host OS IP packets...")

	// 2. Setup gomodem DSP
	allocator := gomem.NewScalableMemoryAllocator(1024 * 1024)
	encoder := protocols.NewAX25Encoder(allocator)
	decoder := protocols.NewAX25Decoder()
	modulator := modem.NewAFSKModulator(44100, 1200, 1200.0, 2200.0)
	demodulator := modem.NewSDFTAFSKDemodulator(1200.0, 2200.0, 1200, 44100)

	// Mock audio channels (Replace with actual ALSA/PortAudio I/O)
	audioTx := make(chan float64, 44100)
	audioRx := make(chan float64, 44100)

	// 3. Goroutine: Receive Audio -> Demodulate -> Write to Host OS
	go func() {
		for sample := range audioRx {
			trigger, bit := demodulator.Demodulate(sample)
			if !trigger {
				continue
			}

			frame := decoder.ProcessBit(bit)
			if frame != nil && len(frame) > 16 {
				// Parse header to find payload (simplified for example)
				headerLen := 14
				for headerLen < len(frame)-4 && frame[headerLen-1]&0x01 == 0 {
					headerLen += 7
				}

				payloadStart := headerLen + 2 // Skip control & PID
				if payloadStart <= len(frame) && frame[payloadStart-1] == PID_IPv4 {
					ipPacket := frame[payloadStart:]
					fmt.Printf("[RX] Received IPv4 packet over radio! (%d bytes). Passing to Host OS.\n", len(ipPacket))

					// Write raw IP packet to the Host OS via the TUN device
					iface.Write(ipPacket)
				}
			}
		}
	}()

	// 4. Main Loop: Read from Host OS -> Modulate -> Send Audio
	packet := make([]byte, 2000)
	for {
		// Read blocks until the Host OS routes a packet to this interface
		n, err := iface.Read(packet)
		if err != nil {
			log.Fatal(err)
		}

		ipPayload := packet[:n]

		// Hardware-level MTU enforcement
		if len(ipPayload) > 256 {
			fmt.Printf("[TX] Dropped %d byte packet (Exceeds 256 MTU)\n", len(ipPayload))
			continue
		}

		fmt.Printf("[TX] Host OS sent IPv4 packet (%d bytes). Modulating to audio...\n", len(ipPayload))

		// Prepend AX.25 IPv4 PID
		ax25Payload := make([]byte, 0, len(ipPayload)+1)
		ax25Payload = append(ax25Payload, PID_IPv4)
		ax25Payload = append(ax25Payload, ipPayload...)

		// Encode
		bitstreamMem := encoder.Encode("HOST", "RADIO", ax25Payload)

		// Modulate
		for _, bit := range bitstreamMem.ToBytes() {
			for range modulator.NextSamplesPerBit() {
				audioTx <- modulator.Modulate(bit)
			}
		}

		bitstreamMem.Reset()
	}
}
