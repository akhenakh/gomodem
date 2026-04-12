package modem_test

import (
	"fmt"
	"testing"

	"github.com/akhenakh/gomodem/modem"
)

func TestModulatorBaudRates(t *testing.T) {
	sampleRate := 44100
	testBits := []byte{1, 0, 1, 1, 0, 0, 1, 0} // Random NRZI test pattern

	tests := []struct {
		baudRate     int
		expectedType string
	}{
		{300, "*modem.AFSKModulator"},
		{1200, "*modem.AFSKModulator"},
		{2400, "*modem.AFSKModulator"},
		{9600, "*modem.GFSKModulator"},
	}

	for _, tt := range tests {
		t.Run("BaudRate_"+string(rune(tt.baudRate)), func(t *testing.T) {
			mod := modem.NewModulatorByBaud(sampleRate, tt.baudRate)

			// Verify we got the correct type from the factory
			actualType := fmt.Sprintf("%T", mod)
			if actualType != tt.expectedType {
				t.Errorf("Expected modulator type %s for %d baud, got %s", tt.expectedType, tt.baudRate, actualType)
			}

			// Ensure fractional sample accumulation doesn't drift
			// e.g. 48000/1200 = 40 (exact)
			// e.g. 44100/1200 = 36.75 (fractional)
			totalSamples := 0
			for _, bit := range testBits {
				samplesForBit := mod.NextSamplesPerBit()
				totalSamples += samplesForBit

				// Pump the bits through to ensure it doesn't panic
				for range samplesForBit {
					val := mod.Modulate(bit)

					// Basic bound checks (-1.0 to 1.0 amplitude)
					if val < -1.01 || val > 1.01 {
						t.Errorf("Modulator output out of bounds: %f", val)
					}
				}
			}

			// For 44100 sample rate, the total samples for exactly 8 bits should be:
			expectedTotalSamples := int((float64(sampleRate) / float64(tt.baudRate)) * float64(len(testBits)))
			// Allow +/- 1 sample tolerance for fractional sample-per-bit ratios
			if totalSamples < expectedTotalSamples-1 || totalSamples > expectedTotalSamples+1 {
				t.Errorf("Timing drift detected! Expected %d total samples for %d bits at %d baud, got %d", expectedTotalSamples, len(testBits), tt.baudRate, totalSamples)
			}

			t.Logf("Successfully modulated 8 bits at %d baud (%d total audio samples)", tt.baudRate, totalSamples)
		})
	}
}

func TestSineFSKModulator(t *testing.T) {
	// Let's also guarantee the SineFSK implementation exists and works
	// for arbitrary high-speed setups.
	sampleRate := 192000
	baudRate := 9600
	mod := modem.NewSineFSKModulator(sampleRate, baudRate)

	samples := mod.NextSamplesPerBit()

	// A steady bit should maintain level
	for range samples {
		val := mod.Modulate(1)
		if val != 0.25 {
			t.Errorf("Sine FSK steady 1 output should be 0.25, got %f", val)
		}
	}
}
