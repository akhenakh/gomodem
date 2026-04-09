package protocols

// AX25Decoder isolates packets between HDLC flags, unstuffs bits, and checks FCS.
type AX25Decoder struct {
	lastNrziLevel byte
	bitstream     []byte
	frameStart    int
	inFrame       bool
}

func NewAX25Decoder() *AX25Decoder {
	return &AX25Decoder{}
}

// ProcessBit accepts a raw NRZI bit. If a full, valid frame is completed,
// it returns the byte payload (including the header, control, PID, but stripping FCS).
func (dec *AX25Decoder) ProcessBit(bit byte) []byte {
	// NRZI Decode
	decoded := byte(1)
	if bit != dec.lastNrziLevel {
		decoded = 0
	}
	dec.lastNrziLevel = bit

	dec.bitstream = append(dec.bitstream, decoded)

	// Check for HDLC Flag (01111110)
	n := len(dec.bitstream)
	if n >= 8 {
		isFlag := dec.bitstream[n-8] == 0 &&
			dec.bitstream[n-7] == 1 && dec.bitstream[n-6] == 1 && dec.bitstream[n-5] == 1 &&
			dec.bitstream[n-4] == 1 && dec.bitstream[n-3] == 1 && dec.bitstream[n-2] == 1 &&
			dec.bitstream[n-1] == 0

		if isFlag {
			if dec.inFrame {
				frameEnd := n - 8
				if frameEnd > dec.frameStart {
					rawBits := dec.bitstream[dec.frameStart:frameEnd]
					frame := dec.decodeRawFrame(rawBits)
					dec.frameStart = n
					if frame != nil {
						return frame
					}
				} else {
					dec.frameStart = n
				}
			} else {
				dec.inFrame = true
				dec.frameStart = n
			}
			return nil
		}
	}

	// Safety trim to prevent memory exhaustion on endless noise
	if n > 8000 {
		dec.bitstream = dec.bitstream[n-8:]
		dec.frameStart = 0
		dec.inFrame = false
	}

	return nil
}

func (dec *AX25Decoder) decodeRawFrame(rawBits []byte) []byte {
	// Bit Unstuffing
	var unstuffed []byte
	ones := 0
	for _, b := range rawBits {
		if b == 1 {
			unstuffed = append(unstuffed, 1)
			ones++
		} else {
			if ones == 5 {
				ones = 0 // Stuffed 0, skip
			} else {
				unstuffed = append(unstuffed, 0)
				ones = 0
			}
		}
	}

	// Bits to Bytes
	if len(unstuffed)%8 != 0 {
		return nil
	}

	frame := make([]byte, len(unstuffed)/8)
	for i := 0; i < len(unstuffed); i += 8 {
		var val byte
		for j := range 8 {
			val |= (unstuffed[i+j] << j)
		}
		frame[i/8] = val
	}

	// FCS Check
	if len(frame) < 4 {
		return nil
	}

	crcData := frame[:len(frame)-2]
	fcsCalc := uint16(0xFFFF)
	for _, b := range crcData {
		for i := range 8 {
			bit := uint16((b >> i) & 1)
			xorIn := (fcsCalc ^ bit) & 1
			fcsCalc >>= 1
			if xorIn != 0 {
				fcsCalc ^= 0x8408
			}
		}
	}
	fcsCalc ^= 0xFFFF

	expectedFCS := uint16(frame[len(frame)-2]) | (uint16(frame[len(frame)-1]) << 8)
	if fcsCalc != expectedFCS {
		return nil
	}

	return frame
}
