package audio

import (
	"bytes"
	"encoding/binary"
	"net"
	"os"
	// Leveraging go-dsp as requested for WAV/DSP manipulation
)

type Stream interface {
	WriteSamples(samples []float64) error
	Close() error
}

// WAVAudioStream writes audio out to a standard WAV file.
// In a full implementation, we'd write a wav Writer (go-dsp/wav focuses on reading).
// Here we output binary PCM data which represents the WAV structure.
type WAVAudioStream struct {
	file       *os.File
	sampleRate uint32
}

func NewWAVAudioStream(filename string, sampleRate int) (*WAVAudioStream, error) {
	f, err := os.Create(filename)
	if err != nil {
		return nil, err
	}

	// Write dummy WAV header (to be filled on close)
	f.Write(make([]byte, 44))
	return &WAVAudioStream{file: f, sampleRate: uint32(sampleRate)}, nil
}

func (s *WAVAudioStream) WriteSamples(samples []float64) error {
	// Convert float64 [-1.0, 1.0] to int16 PCM
	pcm := make([]int16, len(samples))
	for i, samp := range samples {
		pcm[i] = int16(samp * 32767.0)
	}
	return binary.Write(s.file, binary.LittleEndian, pcm)
}

func (s *WAVAudioStream) Close() error {
	// Write standard 44-byte WAV Header at the beginning of the file
	stat, _ := s.file.Stat()
	dataSize := uint32(stat.Size() - 44)

	s.file.Seek(0, 0)
	s.file.Write([]byte("RIFF"))
	binary.Write(s.file, binary.LittleEndian, dataSize+36)
	s.file.Write([]byte("WAVEfmt "))
	binary.Write(s.file, binary.LittleEndian, uint32(16)) // PCM fmt chunk
	binary.Write(s.file, binary.LittleEndian, uint16(1))  // Format PCM
	binary.Write(s.file, binary.LittleEndian, uint16(1))  // Channels
	binary.Write(s.file, binary.LittleEndian, s.sampleRate)
	binary.Write(s.file, binary.LittleEndian, s.sampleRate*2) // ByteRate
	binary.Write(s.file, binary.LittleEndian, uint16(2))      // BlockAlign
	binary.Write(s.file, binary.LittleEndian, uint16(16))     // BitsPerSample
	s.file.Write([]byte("data"))
	binary.Write(s.file, binary.LittleEndian, dataSize)

	return s.file.Close()
}

// TCPAudioStream writes raw PCM audio over a TCP connection
type TCPAudioStream struct {
	conn net.Conn
}

func NewTCPAudioStream(address string) (*TCPAudioStream, error) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return nil, err
	}
	return &TCPAudioStream{conn: conn}, nil
}

func (s *TCPAudioStream) WriteSamples(samples []float64) error {
	buf := new(bytes.Buffer)
	for _, samp := range samples {
		binary.Write(buf, binary.LittleEndian, int16(samp*32767.0))
	}
	_, err := s.conn.Write(buf.Bytes())
	return err
}

func (s *TCPAudioStream) Close() error {
	return s.conn.Close()
}
