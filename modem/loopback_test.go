package modem_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/akhenakh/gomodem/modem"
	"github.com/akhenakh/gomodem/protocols"
	fap "github.com/hessu/go-aprs-fap"
	"github.com/langhuihui/gomem"
	"github.com/madelynnblue/go-dsp/wav" // Using go-dsp/wav for reading
)

// writeWAV Write 32-Bit IEEE Float WAV
func writeWAV(filename string, sampleRate int, samples []float64) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	// Convert float64 (-1.0 to 1.0) to int16 (-32768 to 32767)
	i16Samples := make([]int16, len(samples))
	for i, s := range samples {
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		i16Samples[i] = int16(s * 32767)
	}

	dataSize := uint32(len(i16Samples) * 2) // 2 bytes per int16

	// Build 44-byte WAV header for PCM (AudioFormat = 1)
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], dataSize+36)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)                   // Subchunk1Size
	binary.LittleEndian.PutUint16(header[20:22], 1)                    // AudioFormat (1 = PCM)
	binary.LittleEndian.PutUint16(header[22:24], 1)                    // NumChannels (1 = Mono)
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))   // SampleRate
	binary.LittleEndian.PutUint32(header[28:32], uint32(sampleRate*2)) // ByteRate
	binary.LittleEndian.PutUint16(header[32:34], 2)                    // BlockAlign
	binary.LittleEndian.PutUint16(header[34:36], 16)                   // BitsPerSample
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], dataSize)

	if _, err := f.Write(header); err != nil {
		return err
	}
	return binary.Write(f, binary.LittleEndian, i16Samples)
}

func TestAFSK1200LoopbackAPRS(t *testing.T) {
	sampleRate := 44100
	baudRate := 1200

	// Create the message using go-aprs-fap
	msg := &fap.Message{
		Destination: "N0CALL",
		Text:        "Hello from Quebec lat 46.81228 lng -71.21454",
		ID:          "42",
	}

	bodyStr, err := fap.EncodeMessage(msg)
	if err != nil {
		t.Fatalf("Error encoding FAP message: %v\n", err)
	}

	body := []byte(bodyStr)
	t.Logf("Generated FAP Payload: %s", bodyStr)

	// Encode to AX.25 NRZI Bitstream using the public API
	allocator := gomem.NewScalableMemoryAllocator(1024 * 1024)
	encoder := protocols.NewAX25Encoder(allocator)

	// Encode with From: KK6NXK-3, To: APRS
	bitstreamMem := encoder.Encode("KK6NXK-3", "APRS", body)
	bitstream := bitstreamMem.ToBytes()

	// Modulate to AFSK Audio Float samples
	t.Logf("Modulating %d bits into audio...", len(bitstream))
	mod := modem.NewModulatorByBaud(sampleRate, baudRate)

	var audioBuffer []float64

	// Add key-up delay (300ms of silence for channel idle simulation)
	keyUpDelay := sampleRate / 10 * 3 // ~300ms
	for range keyUpDelay {
		audioBuffer = append(audioBuffer, 0.0)
	}

	for _, nrziBit := range bitstream {
		// API Contract: mod.NextSamplesPerBit() tells us exactly how many audio samples
		// to generate for this specific bit to prevent fractional drift.
		samples := mod.NextSamplesPerBit()
		for range samples {
			// API Contract: mod.Modulate() is stateful. It advances the DDS phase
			// by exactly one sample period (1/48000th of a sec) and returns the amplitude.
			audioBuffer = append(audioBuffer, mod.Modulate(nrziBit))
		}
	}

	// Add tail silence to flush audio buffers in external streaming decoders (like direwolf)
	tailDelay := sampleRate / 10 * 2 // ~200ms
	for range tailDelay {
		audioBuffer = append(audioBuffer, 0.0)
	}

	var maxAmp float64
	for _, s := range audioBuffer {
		if s < 0 {
			s = -s
		}
		if s > maxAmp {
			maxAmp = s
		}
	}
	t.Logf("Max amplitude: %.4f (should be ~0.25)", maxAmp)
	t.Logf("Total audio samples: %d (%.2f seconds)", len(audioBuffer), float64(len(audioBuffer))/float64(sampleRate))

	if maxAmp == 0 || maxAmp > 1.01 {
		t.Fatalf("Modulator output is broken (maxAmp = %f)", maxAmp)
	}

	// Output the audio to a WAV file in the current directory
	wavFilename := "test.wav"
	if err := writeWAV(wavFilename, sampleRate, audioBuffer); err != nil {
		t.Fatalf("Failed to write WAV file: %v", err)
	}
	t.Logf("Successfully wrote modulated audio to %s", wavFilename)

	// READ WAV FILE USING go-dsp/wav
	file, err := os.Open(wavFilename)
	if err != nil {
		t.Fatalf("Failed to open %s: %v", wavFilename, err)
	}
	defer file.Close()

	// Parse the WAV header utilizing go-dsp
	dspWav, err := wav.New(file)
	if err != nil {
		t.Fatalf("go-dsp failed to parse WAV: %v", err)
	}

	t.Logf("go-dsp recognized: %d Hz, %d channels, %d samples",
		dspWav.SampleRate, dspWav.NumChannels, dspWav.Samples)

	// Extract the audio natively as floats using go-dsp
	floats32, err := dspWav.ReadFloats(dspWav.Samples)
	if err != nil {
		t.Fatalf("go-dsp failed to read float samples: %v", err)
	}

	// DEMODULATE & DECODE
	t.Logf("Demodulating %d audio samples read by go-dsp...", len(floats32))
	demodulator := modem.NewSDFTAFSKDemodulator(1200.0, 2200.0, baudRate, sampleRate)
	decoder := protocols.NewAX25Decoder()

	var receivedFrames [][]byte

	for _, sample32 := range floats32 {
		// Feed the sample into the SDFT demodulator
		trigger, bit := demodulator.Demodulate(float64(sample32))

		// If the Gardner PLL indicates a symbol boundary, feed the bit to the decoder
		if trigger {
			frame := decoder.ProcessBit(bit)
			if frame != nil {
				receivedFrames = append(receivedFrames, frame)
			}
		}
	}

	// Verify Results
	if len(receivedFrames) == 0 {
		t.Fatalf("Failed to decode any frames from the audio buffer.")
	}

	t.Logf("Decoded %d frames.", len(receivedFrames))
	recvFrame := receivedFrames[0]

	// Extract the payload (Strip Header + Control + PID + FCS)
	if len(recvFrame) < 18 {
		t.Fatalf("Received frame is too short to be valid AX.25: %d bytes", len(recvFrame))
	}

	// Dynamically parse the AX.25 Address Field
	// AX.25 addresses are 7 bytes long. The last address in the path has bit 0 of its SSID byte (byte 6) set to 1.
	headerLen := 14 // Minimum length (Destination + Source)
	for {
		if headerLen > len(recvFrame)-4 {
			t.Fatalf("Malformed AX.25 frame: could not find end of address field")
		}

		// Check the Extension bit (LSB) of the current address's SSID byte
		if recvFrame[headerLen-1]&0x01 == 1 {
			break // Found the last address in the header
		}
		headerLen += 7 // Move to the next address block
	}

	controlPIDLen := 2 // 1 byte Control + 1 byte PID
	fcsLen := 2        // 2 byte Frame Check Sequence at the end

	payloadStart := headerLen + controlPIDLen
	payloadEnd := len(recvFrame) - fcsLen

	if payloadStart > payloadEnd {
		t.Fatalf("Malformed AX.25 frame: payload bounds invalid")
	}

	extractedPayload := recvFrame[payloadStart:payloadEnd]

	// Compare the extracted payload with the original APRS body
	if !bytes.Equal(extractedPayload, body) {
		t.Errorf("Payload mismatch!\nExpected: %s\nGot: %s", bodyStr, string(extractedPayload))
	} else {
		t.Logf("Success! Loopback payload matched perfectly: %s", string(extractedPayload))
	}
}
