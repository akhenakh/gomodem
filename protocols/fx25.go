package protocols

import (
	"sync"

	"github.com/langhuihui/gomem"
)

// FEC extension to AX25 https://web.archive.org/web/20110718212817/http://www.stensat.org/Docs/FX-25_01_06.pdf

var (
	gfExp  [512]byte
	gfLog  [256]int
	rsGen  []byte
	rsOnce sync.Once
)

// initRS initializes the Galois Field GF(2^8) math tables and the
// Reed-Solomon generator polynomial for RS(255, 239) using the standard
// polynomial p(x) = x^8 + x^4 + x^3 + x^2 + 1 (0x11D).
func initRS() {
	x := 1
	for i := range 255 {
		gfExp[i] = byte(x)
		gfExp[i+255] = byte(x)
		gfLog[x] = i
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}

	// Calculate generator polynomial for 16 check symbols
	rsGen = []byte{1}
	for i := range 16 {
		root := gfExp[i]
		nextGen := make([]byte, len(rsGen)+1)
		for j := 0; j < len(rsGen); j++ {
			nextGen[j+1] ^= rsGen[j]
			nextGen[j] ^= gfMul(rsGen[j], root)
		}
		rsGen = nextGen
	}
}

func gfMul(x, y byte) byte {
	if x == 0 || y == 0 {
		return 0
	}
	return gfExp[gfLog[x]+gfLog[y]]
}

// rsEncode performs polynomial division to generate parity bytes.
func rsEncode(data []byte, gen []byte) []byte {
	// gen[0] is the coefficient for x^0, ..., gen[16] is the coefficient for x^16 = 1
	parity := make([]byte, len(gen)-1)

	for _, b := range data {
		// The incoming byte b is added to the highest degree of the parity register
		feedback := parity[len(parity)-1] ^ b

		// Shift parity left (multiply by x)
		for i := len(parity) - 1; i > 0; i-- {
			parity[i] = parity[i-1]
		}
		parity[0] = 0

		// Add feedback * (G(x) - x^16)
		if feedback != 0 {
			for i := range parity {
				parity[i] ^= gfMul(feedback, gen[i])
			}
		}
	}

	// Reverse parity bytes so that the highest degree (x^15) is first in the returned slice
	// This matches the order expected by the standard Horner's syndrome evaluation.
	out := make([]byte, len(parity))
	for i := range parity {
		out[i] = parity[len(parity)-1-i]
	}
	return out
}

// FX25Encoder handles framing and FEC encoding of FX.25 packets.
type FX25Encoder struct {
	allocator *gomem.ScalableMemoryAllocator
}

func NewFX25Encoder(allocator *gomem.ScalableMemoryAllocator) *FX25Encoder {
	rsOnce.Do(initRS)
	return &FX25Encoder{allocator: allocator}
}

// Encode converts a frame into a raw NRZI bitstream wrapped with FX.25 FEC.
func (enc *FX25Encoder) Encode(from, to string, payload []byte) gomem.Memory {
	// Construct the raw AX.25 Packet
	header := make([]byte, 14)
	copyAddress(header[0:7], to, false)
	copyAddress(header[7:14], from, true)

	frameBytes := make([]byte, 0, 14+2+len(payload)+2)
	frameBytes = append(frameBytes, header...)
	frameBytes = append(frameBytes, 0x03, 0xF0) // UI Frame, No Layer 3
	frameBytes = append(frameBytes, payload...)

	fcs := calculateFCS(frameBytes)
	frameBytes = append(frameBytes, byte(fcs&0xFF), byte(fcs>>8))

	var frameBits []byte
	for _, b := range frameBytes {
		for i := range 8 {
			frameBits = append(frameBits, (b>>i)&1)
		}
	}

	// Bit Stuffing (Insert 0 after five consecutive 1s) ONLY for AX.25 part
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

	var fx25Bits []byte
	fx25Bits = append(fx25Bits, 0, 1, 1, 1, 1, 1, 1, 0) // Start flag
	fx25Bits = append(fx25Bits, stuffedBits...)
	fx25Bits = append(fx25Bits, 0, 1, 1, 1, 1, 1, 1, 0) // End flag

	// Bit-to-Byte packing
	var infoBytes []byte
	var currentByte byte
	var bitCount int
	for _, b := range fx25Bits {
		currentByte |= (b << bitCount)
		bitCount++
		if bitCount == 8 {
			infoBytes = append(infoBytes, currentByte)
			currentByte = 0
			bitCount = 0
		}
	}

	// Pad remainder to byte boundary using MSbs of 0x7E
	if bitCount > 0 {
		padByte := byte(0x7E)
		mask := byte(0xFF << bitCount)
		currentByte |= (padByte & mask)
		infoBytes = append(infoBytes, currentByte)
	}

	// Use Tag_01 for RS(255, 239) => 239 info bytes
	targetInfoLen := 239
	if len(infoBytes) > targetInfoLen {
		// Fallback to pure AX.25 if the frame exceeds single-block bounds
		ax25 := NewAX25Encoder(enc.allocator)
		return ax25.Encode(from, to, payload)
	}

	// Fill remaining information block with 0x7E
	for len(infoBytes) < targetInfoLen {
		infoBytes = append(infoBytes, 0x7E)
	}

	// Calculate Check Symbols
	parity := rsEncode(infoBytes, rsGen)

	// Assemble Final FX.25 Frame
	var fullBits []byte

	// Preamble (4 bytes of 0x7E)
	for range 4 {
		for j := range 8 {
			fullBits = append(fullBits, (0x7E>>j)&1)
		}
	}

	// Correlation Tag_01
	tag01 := []byte{0x3E, 0x2F, 0x53, 0x8A, 0xDF, 0xB7, 0x4D, 0xB7}
	for _, b := range tag01 {
		for j := range 8 {
			fullBits = append(fullBits, (b>>j)&1)
		}
	}

	// FEC Codeblock InfoBytes
	for _, b := range infoBytes {
		for j := range 8 {
			fullBits = append(fullBits, (b>>j)&1)
		}
	}

	// FEC Check Symbols (16 bytes)
	for _, b := range parity {
		for j := range 8 {
			fullBits = append(fullBits, (b>>j)&1)
		}
	}

	// Postamble (2 bytes of 0x7E)
	for range 2 {
		for j := range 8 {
			fullBits = append(fullBits, (0x7E>>j)&1)
		}
	}

	// NRZI Encode the complete sequence
	nrziBuf := enc.allocator.Malloc(len(fullBits))
	currentLevel := byte(0)
	for i, b := range fullBits {
		if b == 0 {
			currentLevel ^= 1
		}
		nrziBuf[i] = currentLevel
	}

	return gomem.NewMemory(nrziBuf)
}
