package modem

import (
	"math"
	"math/cmplx"
)

// BiquadBandpass implements an Infinite Impulse Response (IIR) bandpass filter
// to shape the audio signal and isolate the AFSK tones from noise.
type BiquadBandpass struct {
	b0, b2, a1, a2 float64
	w1, w2         float64
}

func NewBiquadBandpass(fCenter, bandwidth, sampleRate float64) *BiquadBandpass {
	w0 := 2.0 * math.Pi * fCenter / sampleRate
	Q := fCenter / bandwidth
	alpha := math.Sin(w0) / (2.0 * Q)
	a0Inv := 1.0 / (1.0 + alpha)

	return &BiquadBandpass{
		b0: alpha * a0Inv,
		b2: -alpha * a0Inv,
		a1: -2.0 * math.Cos(w0) * a0Inv,
		a2: (1.0 - alpha) * a0Inv,
	}
}

func (f *BiquadBandpass) Process(sample float64) float64 {
	w := sample - f.a1*f.w1 - f.a2*f.w2
	out := f.b0*w + f.b2*f.w2
	f.w2 = f.w1
	f.w1 = w
	return out
}

// PLLGardner implements a Gardner Phase-Locked Loop for symbol timing recovery.
type PLLGardner struct {
	phase, freq, freqNominal, alpha float64
	midSoft, prevSoft               float64
	midCaptured                     bool
}

func NewPLLGardner(samplesPerBit, alpha float64) *PLLGardner {
	freq := 1.0 / samplesPerBit
	return &PLLGardner{
		freq:        freq,
		freqNominal: freq,
		alpha:       alpha,
	}
}

// Advance ticks the PLL. Returns (true, bit) if a symbol boundary is crossed.
func (p *PLLGardner) Advance(soft float64) (bool, byte) {
	p.phase += p.freq

	if !p.midCaptured && p.phase >= 0.5 {
		p.midSoft = soft
		p.midCaptured = true
	}

	if p.phase < 1.0 {
		return false, 0
	}

	p.phase -= 1.0
	p.midCaptured = false

	prevSign := 1.0
	if p.prevSoft <= 0 {
		prevSign = -1.0
	}
	currSign := 1.0
	if soft <= 0 {
		currSign = -1.0
	}

	if prevSign != currSign {
		errVal := p.midSoft * (prevSign - currSign)
		norm := math.Max(math.Abs(p.prevSoft), math.Abs(soft))
		if norm > 0 {
			errVal /= norm
		}
		p.phase += p.alpha * errVal

		maxDev := p.freqNominal * 0.05
		p.freq = math.Max(p.freqNominal-maxDev, math.Min(p.freqNominal+maxDev, p.freq))
	}

	p.prevSoft = soft

	bit := byte(0)
	if soft > 0 {
		bit = 1
	}
	return true, bit
}

// SDFTAFSKDemodulator implements a Sliding DFT AFSK Demodulator.
type SDFTAFSKDemodulator struct {
	window          []float64
	pos, fed        int
	zMark, zSpace   complex128
	twMark, twSpace complex128
	wnMark, wnSpace complex128
	bpf             *BiquadBandpass
	pll             *PLLGardner
}

func NewSDFTAFSKDemodulator(fMark, fSpace float64, baudRate, sampleRate int) *SDFTAFSKDemodulator {
	N := 51
	kMark := fMark * float64(N) / float64(sampleRate)
	kSpace := fSpace * float64(N) / float64(sampleRate)

	fCenter := (fMark + fSpace) / 2.0
	bw := (fSpace - fMark) + 600.0

	return &SDFTAFSKDemodulator{
		window:  make([]float64, N),
		twMark:  cmplx.Exp(complex(0, -2.0*math.Pi*kMark/float64(N))),
		twSpace: cmplx.Exp(complex(0, -2.0*math.Pi*kSpace/float64(N))),
		wnMark:  cmplx.Exp(complex(0, -2.0*math.Pi*kMark)),
		wnSpace: cmplx.Exp(complex(0, -2.0*math.Pi*kSpace)),
		bpf:     NewBiquadBandpass(fCenter, bw, float64(sampleRate)),
		pll:     NewPLLGardner(float64(sampleRate)/float64(baudRate), -0.07),
	}
}

// Demodulate processes an incoming audio sample. If it crosses a symbol boundary,
// it returns true alongside the demodulated bit.
func (d *SDFTAFSKDemodulator) Demodulate(sample float64) (bool, byte) {
	// Biquad Bandpass filtering
	sample = d.bpf.Process(sample)

	N := len(d.window)
	xOld := d.window[d.pos]
	d.window[d.pos] = sample
	d.pos = (d.pos + 1) % N

	d.zMark = d.twMark*d.zMark + complex(sample, 0) - complex(xOld, 0)*d.wnMark
	d.zSpace = d.twSpace*d.zSpace + complex(sample, 0) - complex(xOld, 0)*d.wnSpace

	magMark := real(d.zMark)*real(d.zMark) + imag(d.zMark)*imag(d.zMark)
	magSpace := real(d.zSpace)*real(d.zSpace) + imag(d.zSpace)*imag(d.zSpace)

	markSpaceDiff := magMark - magSpace

	if d.fed < N {
		d.fed++
		return false, 0
	}

	return d.pll.Advance(markSpaceDiff)
}
