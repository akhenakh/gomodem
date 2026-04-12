package protocols

import (
	"testing"

	"github.com/langhuihui/gomem"
)

// TestRSEncodeSyndromes mathematically validates the Reed-Solomon encoder
// by checking the calculated parity against the roots of the generator polynomial.
// For a valid Reed-Solomon codeword, evaluating the polynomial at each root
// must yield exactly zero (a syndrome of 0).
func TestRSEncodeSyndromes(t *testing.T) {
	// Ensure math tables are initialized
	rsOnce.Do(initRS)

	// Generate arbitrary data matching the RS(255, 239) info size
	data := make([]byte, 239)
	for i := range data {
		data[i] = byte(i % 256)
	}

	// Generate the 16 parity bytes
	parity := rsEncode(data, rsGen)
	if len(parity) != 16 {
		t.Fatalf("Expected 16 parity bytes, got %d", len(parity))
	}

	// Combine data and parity to form the full codeword
	codeword := append(data, parity...)

	// Calculate the 16 syndromes (evaluate the polynomial at roots a^0 through a^15)
	for i := range 16 {
		root := gfExp[i]
		val := byte(0)
		// Horner's method to evaluate the polynomial
		for _, b := range codeword {
			val = gfMul(val, root) ^ b
		}

		if val != 0 {
			t.Errorf("Syndrome %d evaluated to non-zero: %v", i, val)
		}
	}
}

// nrziDecode is a helper function that reverses the NRZI encoding (0 = toggle, 1 = steady)
func nrziDecode(nrziBits []byte) []byte {
	var decoded []byte
	currentLevel := byte(0)
	for _, b := range nrziBits {
		if b != currentLevel {
			decoded = append(decoded, 0)
		} else {
			decoded = append(decoded, 1)
		}
		currentLevel = b
	}
	return decoded
}

// TestFX25Encoder verifies that the encoder produces a valid bitstream containing
// the Preamble, the Tag_01 Correlation Tag, and successfully NRZI encodes the output.
func TestFX25Encoder(t *testing.T) {
	allocator := gomem.NewScalableMemoryAllocator(1024 * 1024)
	enc := NewFX25Encoder(allocator)

	// Encode a test payload
	mem := enc.Encode("KK6NXK-3", "APRS", []byte("Hello FX.25 Link Protocol!"))
	defer mem.Reset()

	bits := mem.ToBytes()
	if len(bits) == 0 {
		t.Fatalf("Encoded bitstream is empty")
	}

	// Decode the NRZI to get raw bit layout
	decodedBits := nrziDecode(bits)

	// Construct the Tag_01 bits representation (LSB first for each byte)
	tag01 := []byte{0x3E, 0x2F, 0x53, 0x8A, 0xDF, 0xB7, 0x4D, 0xB7}
	var tagBits []byte
	for _, b := range tag01 {
		for j := range 8 {
			tagBits = append(tagBits, (b>>j)&1)
		}
	}

	// Search the stream for the Tag_01 correlation tag
	foundTag := false
	tagStartIndex := -1
	for i := 0; i <= len(decodedBits)-len(tagBits); i++ {
		match := true
		for j := 0; j < len(tagBits); j++ {
			if decodedBits[i+j] != tagBits[j] {
				match = false
				break
			}
		}
		if match {
			foundTag = true
			tagStartIndex = i
			break
		}
	}

	if !foundTag {
		t.Fatalf("FX.25 Tag_01 Correlation Code not found in the decoded bitstream")
	}

	t.Logf("Successfully located Tag_01 at bit index %d", tagStartIndex)

	// Ensure there are enough preamble flag bits (4 bytes of 0x7E) before the tag
	if tagStartIndex < 32 {
		t.Errorf("Tag started at index %d, expected at least 32 bits of preamble space", tagStartIndex)
	}

	// Verify the total length accounts for padding to 255 bytes + wrapper length
	// Pre(4) + Tag(8) + Info(239) + Parity(16) + Post(2) = 269 bytes = 2152 bits
	if len(decodedBits) != 2152 {
		t.Errorf("Expected full frame bit length of 2152, got %d", len(decodedBits))
	}
}
