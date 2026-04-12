package protocols

import (
	"github.com/langhuihui/gomem"
)

// AX25Encoder handles framing and encoding of AX25 packets.
type AX25Encoder struct {
	allocator *gomem.ScalableMemoryAllocator
}

func NewAX25Encoder(allocator *gomem.ScalableMemoryAllocator) *AX25Encoder {
	return &AX25Encoder{allocator: allocator}
}

// Encode converts a frame into a raw NRZI bitstream using gomem.
func (enc *AX25Encoder) Encode(from, to string, payload []byte) gomem.Memory {
	// Construct AX.25 Header (Addresses are 7 bytes: 6 chars shifted left 1, 1 byte SSID)
	header := make([]byte, 14)
	copyAddress(header[0:7], to, false)
	copyAddress(header[7:14], from, true) // True if last in path

	frameBytes := make([]byte, 0, 14+2+len(payload)+2)
	frameBytes = append(frameBytes, header...)
	frameBytes = append(frameBytes, 0x03, 0xF0) // UI Frame, No Layer 3
	frameBytes = append(frameBytes, payload...)

	// Calculate FCS (CRC-16-CCITT)
	fcs := calculateFCS(frameBytes)
	frameBytes = append(frameBytes, byte(fcs&0xFF), byte(fcs>>8))

	// Convert Frame Bytes to NRZ Bits (LSB first)
	var frameBits []byte
	for _, b := range frameBytes {
		for i := range 8 {
			frameBits = append(frameBits, (b>>i)&1)
		}
	}

	// Bit Stuffing (Insert 0 after five consecutive 1s)
	var stuffedBits []byte
	onesCount := 0
	for _, b := range frameBits {
		stuffedBits = append(stuffedBits, b)
		if b == 1 {
			onesCount++
			if onesCount == 5 {
				stuffedBits = append(stuffedBits, 0)
				onesCount = 0
			}
		} else {
			onesCount = 0
		}
	}

	// Wrap with HDLC Flags (0x7E = 01111110 LSB first)
	var fullBits []byte
	for range 50 { // Preamble flags (approx 333ms TXDELAY for receiver PLL sync)
		fullBits = append(fullBits, 0, 1, 1, 1, 1, 1, 1, 0)
	}
	fullBits = append(fullBits, stuffedBits...)
	for range 5 { // Postamble flags
		fullBits = append(fullBits, 0, 1, 1, 1, 1, 1, 1, 0)
	}

	// NRZI Encode the complete sequence (0 = toggle, 1 = keep level)
	// We use the ScalableMemoryAllocator for the final bitstream
	nrziBuf := enc.allocator.Malloc(len(fullBits))
	currentLevel := byte(0) // Start at low level

	for i, b := range fullBits {
		if b == 0 {
			currentLevel ^= 1
		}
		nrziBuf[i] = currentLevel
	}

	return gomem.NewMemory(nrziBuf)
}

func copyAddress(dst []byte, callsign string, last bool) {
	for i := range 6 {
		if i < len(callsign) {
			dst[i] = callsign[i] << 1
		} else {
			dst[i] = ' ' << 1
		}
	}
	dst[6] = 0x60 // SSID 0, reserved bits
	if last {
		dst[6] |= 0x01
	}
}

func calculateFCS(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		for i := range 8 {
			bit := uint16((b >> i) & 1)
			xorIn := (crc ^ bit) & 1
			crc >>= 1
			if xorIn != 0 {
				crc ^= 0x8408
			}
		}
	}
	return crc ^ 0xFFFF
}
