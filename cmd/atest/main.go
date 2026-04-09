package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/akhenakh/gomodem/modem"
	"github.com/akhenakh/gomodem/protocols"
)

type WAVHeader struct {
	ChunkID       [4]byte
	ChunkSize     uint32
	Format        [4]byte
	Subchunk1ID   [4]byte
	Subchunk1Size uint32
	AudioFormat   uint16
	NumChannels   uint16
	SampleRate    uint32
	ByteRate      uint32
	BlockAlign    uint16
	BitsPerSample uint16
}

type Config struct {
	BitsPerSec int
	PrintHex   bool
	FixFrames  int
	SampleRate int
	Channel    int
	MinDecoded int
	MaxDecoded int
}

func main() {
	cfg := &Config{}

	flag.IntVar(&cfg.BitsPerSec, "B", 1200, "Bits/second for data rate (300, 1200, 2400, 4800, 9600)")
	flag.BoolVar(&cfg.PrintHex, "h", false, "Print frame contents as hexadecimal bytes")
	flag.IntVar(&cfg.FixFrames, "F", 0, "Amount of effort to fix frames with invalid CRC (0=none, 1=single bit)")
	flag.IntVar(&cfg.SampleRate, "R", 0, "Override sample rate (0=auto from WAV)")
	flag.IntVar(&cfg.Channel, "C", 0, "Audio channel: 0=left, 1=right, 2=both")
	flag.IntVar(&cfg.MinDecoded, "L", 0, "Error if less than this number decoded")
	flag.IntVar(&cfg.MaxDecoded, "G", 0, "Error if greater than this number decoded")

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] wav-file-in\n\n", filepath.Base(os.Args[0]))
		fmt.Fprintf(flag.CommandLine.Output(), "atest decodes AX.25 frames from audio recordings.\n\n")
		fmt.Fprintf(flag.CommandLine.Output(), "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(flag.CommandLine.Output(), "\nExamples:\n")
		fmt.Fprintf(flag.CommandLine.Output(), "  %s test.wav\n", filepath.Base(os.Args[0]))
		fmt.Fprintf(flag.CommandLine.Output(), "  %s -B 300 test.wav\n", filepath.Base(os.Args[0]))
		fmt.Fprintf(flag.CommandLine.Output(), "  %s -B 9600 -h test.wav\n", filepath.Base(os.Args[0]))
	}

	flag.Parse()

	if flag.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "Error: wav-file-in is required")
		flag.Usage()
		os.Exit(1)
	}

	filename := flag.Arg(0)

	decoded, err := decodeWAVFile(filename, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if cfg.MinDecoded > 0 && decoded < cfg.MinDecoded {
		fmt.Fprintf(os.Stderr, "Error: decoded %d frames, expected at least %d\n", decoded, cfg.MinDecoded)
		os.Exit(1)
	}

	if cfg.MaxDecoded > 0 && decoded > cfg.MaxDecoded {
		fmt.Fprintf(os.Stderr, "Error: decoded %d frames, expected at most %d\n", decoded, cfg.MaxDecoded)
		os.Exit(1)
	}
}

func decodeWAVFile(filename string, cfg *Config) (int, error) {
	f, err := os.Open(filename)
	if err != nil {
		return 0, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	var hdr WAVHeader
	if err := binary.Read(f, binary.LittleEndian, &hdr); err != nil {
		return 0, fmt.Errorf("failed to read WAV header: %w", err)
	}

	if string(hdr.ChunkID[:]) != "RIFF" {
		return 0, fmt.Errorf("not a valid WAV file (missing RIFF)")
	}
	if string(hdr.Format[:]) != "WAVE" {
		return 0, fmt.Errorf("not a valid WAV file (missing WAVE)")
	}

	sampleRate := int(hdr.SampleRate)
	if cfg.SampleRate > 0 {
		sampleRate = cfg.SampleRate
	}

	baudRate := cfg.BitsPerSec
	if baudRate != 300 && baudRate != 1200 && baudRate != 2400 && baudRate != 4800 && baudRate != 9600 {
		fmt.Fprintf(os.Stderr, "Warning: Unusual baud rate %d, using default tones\n", baudRate)
	}

	var fMark, fSpace float64
	switch baudRate {
	case 300:
		fMark = 1600
		fSpace = 1800
	case 1200:
		fMark = 1200
		fSpace = 2200
	default:
		fMark = 1200
		fSpace = 2200
	}

	fmt.Fprintf(os.Stderr, "File: %s\n", filename)
	fmt.Fprintf(os.Stderr, "Sample Rate: %d Hz\n", sampleRate)
	fmt.Fprintf(os.Stderr, "Channels: %d\n", hdr.NumChannels)
	fmt.Fprintf(os.Stderr, "Bits/Sample: %d\n", hdr.BitsPerSample)
	fmt.Fprintf(os.Stderr, "Baud Rate: %d bps\n", baudRate)
	fmt.Fprintf(os.Stderr, "AFSK Tones: %.0f / %.0f Hz\n\n", fMark, fSpace)

	var dataID [4]byte
	var dataSize uint32
	for {
		if err := binary.Read(f, binary.LittleEndian, &dataID); err != nil {
			return 0, fmt.Errorf("failed to find data chunk: %w", err)
		}
		if err := binary.Read(f, binary.LittleEndian, &dataSize); err != nil {
			return 0, fmt.Errorf("failed to read data size: %w", err)
		}
		if string(dataID[:]) == "data" {
			break
		}
		if _, err := f.Seek(int64(dataSize), 1); err != nil {
			return 0, fmt.Errorf("failed to skip chunk: %w", err)
		}
	}

	bytesPerSample := int(hdr.BitsPerSample) / 8
	numChannels := int(hdr.NumChannels)

	channelStart := 0
	channelEnd := numChannels
	if cfg.Channel == 0 {
		channelStart = 0
		channelEnd = 1
	} else if cfg.Channel == 1 {
		channelStart = 1
		channelEnd = 2
	}

	buffer := make([]byte, 4096)
	sampleBuffer := make([]float64, 0, 4096)

	decoders := make([]*protocols.AX25Decoder, channelEnd-channelStart)
	demodulators := make([]*modem.SDFTAFSKDemodulator, channelEnd-channelStart)

	for i := range decoders {
		decoders[i] = protocols.NewAX25Decoder()
		demodulators[i] = modem.NewSDFTAFSKDemodulator(fMark, fSpace, baudRate, sampleRate)
	}

	totalDecoded := 0
	frameNum := 0
	samplesProcessed := 0

	for {
		n, err := f.Read(buffer)
		if n == 0 {
			break
		}
		if err != nil {
			break
		}

		for i := 0; i < n; i += bytesPerSample * numChannels {
			if i+bytesPerSample*numChannels > n {
				break
			}

			for ch := channelStart; ch < channelEnd; ch++ {
				var sample float64
				offset := i + ch*bytesPerSample

				if bytesPerSample == 2 {
					val := int16(binary.LittleEndian.Uint16(buffer[offset : offset+2]))
					sample = float64(val) / 32768.0
				} else if bytesPerSample == 1 {
					val := buffer[offset]
					sample = (float64(val) - 128.0) / 128.0
				}

				sampleBuffer = append(sampleBuffer, sample)
			}
		}

		for idx := range decoders {
			for _, sample := range sampleBuffer[idx:len(sampleBuffer):len(sampleBuffer)] {
				samplesProcessed++
				hasBit, bit := demodulators[idx].Demodulate(sample)
				if !hasBit {
					continue
				}

				frame := decoders[idx].ProcessBit(bit)
				if len(frame) > 0 {
					frameNum++
					totalDecoded++
					printFrame(frame, frameNum, cfg.PrintHex)
				}
			}
		}

		sampleBuffer = sampleBuffer[:0]
	}

	fmt.Fprintf(os.Stderr, "\n---\nDecoded %d frames from %d samples\n", totalDecoded, samplesProcessed)
	return totalDecoded, nil
}

func printFrame(frame []byte, frameNum int, printHex bool) {
	fmt.Printf("Frame #%d (%d bytes):\n", frameNum, len(frame))

	if printHex {
		for i, b := range frame {
			fmt.Printf("%02X ", b)
			if (i+1)%16 == 0 {
				fmt.Println()
			}
		}
		if len(frame)%16 != 0 {
			fmt.Println()
		}
	} else {
		if len(frame) >= 7 {
			dest := parseCallsign(frame[0:7])
			src := parseCallsign(frame[7:14])
			fmt.Printf("  From: %s\n", src)
			fmt.Printf("  To: %s\n", dest)

			if len(frame) >= 16 {
				info := frame[16:]
				if len(info) > 0 {
					fmt.Printf("  Info: %q\n", string(info))
				}
			}
		} else {
			fmt.Printf("  Raw: %X\n", frame)
		}
	}

	fmt.Println()
}

func parseCallsign(b []byte) string {
	if len(b) < 6 {
		return "UNKNOWN"
	}

	var callsign []byte
	for i := 0; i < 6; i++ {
		c := b[i] >> 1
		if c >= 32 && c < 127 {
			callsign = append(callsign, c)
		}
	}

	ssid := (b[6] >> 1) & 0x0F

	result := string(callsign)
	for len(result) > 0 && result[len(result)-1] == ' ' {
		result = result[:len(result)-1]
	}

	if ssid != 0 {
		result = fmt.Sprintf("%s-%d", result, ssid)
	}

	return result
}
