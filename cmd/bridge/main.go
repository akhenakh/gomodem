package main

import (
	"fmt"
	"net"

	"github.com/akhenakh/gomodem/modem"
	"github.com/akhenakh/gomodem/protocols"
	"github.com/containers/gvisor-tap-vsock/pkg/tap"
	"github.com/containers/gvisor-tap-vsock/pkg/types"
	"github.com/langhuihui/gomem"
	"gvisor.dev/gvisor/pkg/tcpip"
)

func main() {
	fmt.Println("Starting gvisor-tap-vsock AX.25 Radio Gateway...")

	// Configure the Virtual Network
	// CRITICAL: MTU must be tiny (256 bytes) so IP packets fit inside AX.25 frames
	// without being shredded by radio static over a slow 1200 baud connection.
	_ = &types.Configuration{
		MTU:               256,
		GatewayIP:         "192.168.127.1",
		GatewayMacAddress: "5a:94:ef:e4:0c:dd",
		Subnet:            "192.168.127.0/24",
	}

	// Create the gvisor-tap-vsock Switch
	netSwitch := tap.NewSwitch(true) // debug mode enabled

	allocator := gomem.NewScalableMemoryAllocator(1024 * 1024)

	mac, err := net.ParseMAC("5a:94:ef:e4:0c:ee")
	if err != nil {
		panic(err)
	}

	// Create our Custom AX.25 Radio Network Device
	radioDevice := &AX25RadioDevice{
		ip:          "192.168.127.2",
		mac:         tcpip.LinkAddress(mac.String()),
		netSwitch:   netSwitch,
		allocator:   allocator,
		encoder:     protocols.NewAX25Encoder(allocator),
		decoder:     protocols.NewAX25Decoder(),
		modulator:   modem.NewAFSKModulator(44100, 1200, 1200.0, 2200.0),
		demodulator: modem.NewSDFTAFSKDemodulator(1200.0, 2200.0, 1200, 44100),
		AudioTx:     make(chan float64, 44100),
	}

	// Attach the Radio Device to the Switch
	netSwitch.Connect(radioDevice)

	// Start listening to incoming audio (Mock loopback for testing)
	go radioDevice.ReceiveAudioStream(radioDevice.AudioTx)

	// In a real scenario, you'd bind `gvproxy` via gvisor-tap-vsock's
	// Unix/TCP socket API here so a VM could connect to the switch.

	// Keep alive
	select {}
}
