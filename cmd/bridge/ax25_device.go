package main

import (
	"fmt"

	"github.com/akhenakh/gomodem/modem"
	"github.com/akhenakh/gomodem/protocols"
	"github.com/containers/gvisor-tap-vsock/pkg/tap"
	"github.com/langhuihui/gomem"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

const (
	// Standard AX.25 Protocol Identifiers (PID) for Layer 3 Routing
	PID_IPv4 = 0xCC
	PID_ARP  = 0x87
	PID_IPv6 = 0x8E
)

// AX25RadioDevice bridges gVisor IP packets to gomodem Audio AFSK tones
type AX25RadioDevice struct {
	ip        string
	mac       tcpip.LinkAddress
	netSwitch *tap.Switch

	// gomodem DSP components
	allocator   *gomem.ScalableMemoryAllocator
	encoder     *protocols.AX25Encoder
	decoder     *protocols.AX25Decoder
	modulator   modem.Modulator
	demodulator *modem.SDFTAFSKDemodulator

	// Audio I/O channels (could be replaced with real PortAudio/ALSA streams)
	AudioTx chan float64
}

// DeliverNetworkPacket is called by the gvisor-tap-vsock Switch when the VM/Container
// sends an IP packet bound for the outside world.
func (d *AX25RadioDevice) DeliverNetworkPacket(protocol tcpip.NetworkProtocolNumber, pkt *stack.PacketBuffer) {
	// We only support IPv4 over this simple radio link
	if protocol != header.IPv4ProtocolNumber {
		return
	}

	// Extract the raw IP Packet bytes from gVisor's internal buffer
	// Note: gVisor's PacketBuffer API changes often, this extracts a flat byte slice
	ipPayload := pkt.ToView().AsSlice()
	if len(ipPayload) == 0 {
		return
	}

	// Prepend the AX.25 PID byte to tell the receiving TNC it's an IPv4 packet
	ax25Payload := make([]byte, 0, len(ipPayload)+1)
	ax25Payload = append(ax25Payload, PID_IPv4)
	ax25Payload = append(ax25Payload, ipPayload...)

	fmt.Printf("[Radio TX] Modulating IPv4 Packet (%d bytes) to 1200 Baud AFSK...\n", len(ipPayload))

	// Encode to NRZI Bitstream
	bitstreamMem := d.encoder.Encode("GVISOR", "RADIO", ax25Payload)
	bitstream := bitstreamMem.ToBytes()

	// Modulate to Audio
	// In a real application, you would batch these and write to an ALSA/PulseAudio/TCP stream
	for _, bit := range bitstream {
		samples := d.modulator.NextSamplesPerBit()
		for range samples {
			d.AudioTx <- d.modulator.Modulate(bit)
		}
	}

	// Free DSP memory
	bitstreamMem.Reset()
}

// ReceiveAudioStream runs in a goroutine, constantly listening to incoming audio
// from the radio. When it decodes a valid AX.25 packet containing an IP packet,
// it injects it back into the gVisor network switch.
func (d *AX25RadioDevice) ReceiveAudioStream(audioRx <-chan float64) {
	for sample := range audioRx {
		trigger, bit := d.demodulator.Demodulate(sample)
		if !trigger {
			continue
		}

		frame := d.decoder.ProcessBit(bit)
		if len(frame) > 16 {
			// Find the end of the AX.25 Address Header
			headerLen := 14
			for {
				if headerLen > len(frame)-4 {
					break
				}
				if frame[headerLen-1]&0x01 == 1 {
					break
				}
				headerLen += 7
			}

			payloadStart := headerLen + 2 // Bypass Control byte and PID byte
			if payloadStart <= len(frame) {
				pid := frame[payloadStart-1]

				// If it's an incoming IPv4 Packet, push it to the gVisor VM
				if pid == PID_IPv4 {
					ipData := frame[payloadStart:]
					fmt.Printf("[Radio RX] Demodulated IPv4 Packet (%d bytes), pushing to gVisor!\n", len(ipData))

					// Reconstruct the gVisor PacketBuffer
					pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{
						Payload: buffer.MakeWithData(ipData),
					})

					// Inject into the Switch
					d.netSwitch.DeliverNetworkPacket(header.IPv4ProtocolNumber, pkt)
				}
			}
		}
	}
}

func (d *AX25RadioDevice) LinkAddress() tcpip.LinkAddress {
	return d.mac
}

func (d *AX25RadioDevice) IP() string {
	return d.ip
}
