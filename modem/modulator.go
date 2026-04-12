package modem

import (
	"math"
)

// Modulator is the unified interface for all packet radio modulators.
type Modulator interface {
	// Modulate takes a single bit (0 or 1) and returns the next audio/baseband sample.
	// It must be called `NextSamplesPerBit()` times per bit.
	Modulate(bit byte) float64

	// NextSamplesPerBit returns the exact number of samples the current bit
	// should be held for, accounting for fractional accumulation to prevent drift.
	NextSamplesPerBit() int

	// Reset clears the phase and delay lines for a new transmission.
	Reset()
}

// AFSK MODULATOR (300, 1200, 2400 baud)

// AFSKModulator implements Direct Digital Synthesis (DDS) AFSK modulation.
type AFSKModulator struct {
	FMark, FSpace float64
	SampleRate    int
	BaudRate      int
	Alpha         float64
	Volume        float64

	freqSmooth    float64
	phase         float64
	samplesPerBit float64
	samplesErr    float64
}

// NewAFSKModulator creates a standard Bell 202 AFSK modulator (1200/2200 Hz).
func NewAFSKModulator(sampleRate, baudRate int, fMark, fSpace float64) *AFSKModulator {
	return &AFSKModulator{
		FMark:         fMark,
		FSpace:        fSpace,
		SampleRate:    sampleRate,
		BaudRate:      baudRate,
		Alpha:         0.5,  // Reduced for smoother transitions (less HF splatter)
		Volume:        0.25, // Adjusted to match typical reference volume (~50 direwolf level)
		freqSmooth:    fMark,
		samplesPerBit: float64(sampleRate) / float64(baudRate),
	}
}

func (m *AFSKModulator) Modulate(bit byte) float64 {
	targetFreq := m.FSpace
	if bit == 1 {
		targetFreq = m.FMark
	}

	m.freqSmooth = m.Alpha*targetFreq + (1.0-m.Alpha)*m.freqSmooth
	m.phase = math.Mod(m.phase+2.0*math.Pi*m.freqSmooth/float64(m.SampleRate), 2.0*math.Pi)

	// math.Sin instead of math.Cos guarantees the signal will cleanly start at 0
	// rather than full amplitude, eliminating key-up transient clicks.
	return math.Sin(m.phase) * m.Volume
}

func (m *AFSKModulator) NextSamplesPerBit() int {
	v := m.samplesPerBit + m.samplesErr
	n := int(math.Round(v))
	m.samplesErr = v - float64(n)
	return n
}

func (m *AFSKModulator) Reset() {
	m.freqSmooth = m.FMark
	m.phase = 0.0
	m.samplesErr = 0.0
}

// GFSK MODULATOR (9600 baud G3RUH)

// GFSKModulator implements a Gaussian lowpass FIR filter on NRZ data.
type GFSKModulator struct {
	firCoeffs     []float64
	delayLine     []float64
	firTaps       int
	delayPos      int
	samplesPerBit float64
	samplesErr    float64
	Volume        float64
}

// NewGFSKModulator creates a 9600 baud standard G3RUH modulator with BT=0.5.
func NewGFSKModulator(sampleRate, baudRate int, bt float64) *GFSKModulator {
	samplesPerBit := float64(sampleRate) / float64(baudRate)

	spanBits := 4.0
	nTaps := int(spanBits*samplesPerBit + 0.5)
	if nTaps < 9 {
		nTaps = 9
	}
	if nTaps%2 == 0 {
		nTaps++ // Force odd for symmetric FIR
	}

	m := &GFSKModulator{
		firCoeffs:     make([]float64, nTaps),
		delayLine:     make([]float64, nTaps),
		firTaps:       nTaps,
		samplesPerBit: samplesPerBit,
		Volume:        0.25,
	}

	// Calculate Gaussian FIR coefficients
	sum := 0.0
	center := float64(nTaps-1) / 2.0
	for n := 0; n < nTaps; n++ {
		t := (float64(n) - center) / samplesPerBit
		h := math.Exp(-2.0 * math.Pi * math.Pi * bt * bt * t * t)
		m.firCoeffs[n] = h
		sum += h
	}

	// Normalize so DC gain = 1.0
	for n := 0; n < nTaps; n++ {
		m.firCoeffs[n] /= sum
	}

	return m
}

func (m *GFSKModulator) Modulate(bit byte) float64 {
	nrz := -1.0
	if bit == 1 {
		nrz = 1.0
	}

	m.delayLine[m.delayPos] = nrz

	output := 0.0
	pos := m.delayPos

	for i := 0; i < m.firTaps; i++ {
		output += m.firCoeffs[i] * m.delayLine[pos]
		pos--
		if pos < 0 {
			pos = m.firTaps - 1
		}
	}

	m.delayPos++
	if m.delayPos >= m.firTaps {
		m.delayPos = 0
	}

	return output * m.Volume
}

func (m *GFSKModulator) NextSamplesPerBit() int {
	v := m.samplesPerBit + m.samplesErr
	n := int(math.Round(v))
	m.samplesErr = v - float64(n)
	return n
}

func (m *GFSKModulator) Reset() {
	for i := range m.delayLine {
		m.delayLine[i] = 0.0
	}
	m.delayPos = 0
	m.samplesErr = 0.0
}

// NewModulatorByBaud is a convenience factory that returns the standard
// modulator configuration for a given packet radio baud rate.
func NewModulatorByBaud(sampleRate, baudRate int) Modulator {
	switch baudRate {
	case 300:
		// HF Packet standard: 1600 Hz Mark, 1800 Hz Space (200 Hz shift)
		return NewAFSKModulator(sampleRate, 300, 1600.0, 1800.0)
	case 1200:
		// VHF Packet standard (Bell 202): 1200 Hz Mark, 2200 Hz Space
		return NewAFSKModulator(sampleRate, 1200, 1200.0, 2200.0)
	case 2400:
		// Generic 2400 baud AFSK (Often uses Phase modulation, but AFSK is common)
		return NewAFSKModulator(sampleRate, 2400, 1200.0, 2400.0)
	case 9600:
		// UHF Packet standard (G3RUH): Baseband GFSK with BT=0.5
		return NewGFSKModulator(sampleRate, 9600, 0.5)
	default:
		// Fallback to 1200
		return NewAFSKModulator(sampleRate, 1200, 1200.0, 2200.0)
	}
}

// SINE FSK MODULATOR
type SineFSKModulator struct {
	transitionTable []float64
	level           float64
	sampleIndex     int
	currentBitSamps int
	transitioning   bool
	samplesPerBit   float64
	samplesErr      float64
	Volume          float64
}

// NewSineFSKModulator creates an FSK modulator that uses a half-cosine transition.
func NewSineFSKModulator(sampleRate, baudRate int) *SineFSKModulator {
	samplesPerBit := float64(sampleRate) / float64(baudRate)
	n := int(samplesPerBit + 0.5)

	table := make([]float64, n)
	for i := 0; i < n; i++ {
		table[i] = math.Cos(math.Pi * float64(i) / float64(n))
	}

	return &SineFSKModulator{
		transitionTable: table,
		level:           1.0,
		currentBitSamps: n,
		samplesPerBit:   samplesPerBit,
		Volume:          0.25,
	}
}

func (m *SineFSKModulator) Modulate(bit byte) float64 {
	if m.sampleIndex == 0 {
		target := -1.0
		if bit == 1 {
			target = 1.0
		}
		m.transitioning = (target != m.level)
		if !m.transitioning {
			m.level = target
		}
	}

	var output float64
	if m.transitioning {
		tableSize := len(m.transitionTable)
		fractionalIdx := float64(m.sampleIndex) * float64(tableSize-1) / float64(m.currentBitSamps-1)
		lower := int(fractionalIdx)
		blend := fractionalIdx - float64(lower)

		var curve float64
		if lower >= tableSize-1 {
			curve = m.transitionTable[tableSize-1]
		} else {
			curve = m.transitionTable[lower]*(1.0-blend) + m.transitionTable[lower+1]*blend
		}

		if m.level > 0.0 {
			output = curve
		} else {
			output = -curve
		}

		if m.sampleIndex == m.currentBitSamps-1 {
			m.level = -m.level
		}
	} else {
		output = m.level
	}

	m.sampleIndex++
	if m.sampleIndex >= m.currentBitSamps {
		m.sampleIndex = 0
	}

	return output * m.Volume
}

func (m *SineFSKModulator) NextSamplesPerBit() int {
	v := m.samplesPerBit + m.samplesErr
	n := int(math.Round(v))
	m.samplesErr = v - float64(n)
	m.currentBitSamps = n
	return n
}

func (m *SineFSKModulator) Reset() {
	m.level = 1.0
	m.sampleIndex = 0
	m.transitioning = false
	m.samplesErr = 0.0
	m.currentBitSamps = len(m.transitionTable)
}
