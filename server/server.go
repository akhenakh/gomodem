package grpc

import (
	"context"
	"fmt"
	"strings"

	"github.com/akhenakh/gomodem/audio"
	pb "github.com/akhenakh/gomodem/gen/modemsvc/v1"
	"github.com/akhenakh/gomodem/modem"
	"github.com/akhenakh/gomodem/protocols"
	"github.com/langhuihui/gomem"
)

type ModemServer struct {
	pb.UnimplementedModemServiceServer
	allocator *gomem.ScalableMemoryAllocator
}

func NewModemServer() *ModemServer {
	// Scalable memory allocator with 1MB blocks to prevent GC churn during DSP
	return &ModemServer{
		allocator: gomem.NewScalableMemoryAllocator(1024 * 1024),
	}
}

func (s *ModemServer) Transmit(ctx context.Context, req *pb.TransmitRequest) (*pb.TransmitResponse, error) {
	fmt.Printf("Transmitting %d bytes from %s to %s\n", len(req.Payload), req.FromCallsign, req.ToCallsign)

	// Encode packet to bitstream
	var bitstream gomem.Memory
	switch req.Protocol {
	case pb.Protocol_AX25:
		encoder := protocols.NewAX25Encoder(s.allocator)
		bitstream = encoder.Encode(req.FromCallsign, req.ToCallsign, req.Payload)
	case pb.Protocol_FX25, pb.Protocol_IL2P:
		// Placeholders for FX25 (Reed Solomon) and IL2P (Galois Scrambler)
		return nil, fmt.Errorf("protocol %v not fully implemented in this example", req.Protocol)
	}

	// Open Audio Target
	var outStream audio.Stream
	var err error

	if after, ok := strings.CutPrefix(req.AudioTarget, "wav:"); ok {
		filename := after
		outStream, err = audio.NewWAVAudioStream(filename, 44100)
	} else if after0, ok0 := strings.CutPrefix(req.AudioTarget, "tcp:"); ok0 {
		address := after0
		outStream, err = audio.NewTCPAudioStream(address)
	} else {
		return nil, fmt.Errorf("unsupported audio target: %s", req.AudioTarget)
	}

	if err != nil {
		return nil, err
	}
	defer outStream.Close()

	// Modulate
	// Support different modulations (AFSK)
	afsk := modem.NewAFSKModulator(44100, int(req.BaudRate), 1200.0, 2200.0)

	// Create a reader to sequentially process bits using gomem
	reader := bitstream.NewReader()

	bitstreamSize := bitstream.Size
	samplesPerBitEst := 44100 / int(req.BaudRate)
	estimatedSamples := bitstreamSize * samplesPerBitEst
	audioBuf := make([]float64, 0, estimatedSamples)

	for {
		b, err := reader.ReadByte()
		if err != nil {
			break // EOF
		}

		samplesPerBit := afsk.NextSamplesPerBit()
		for range samplesPerBit {
			samp := afsk.Modulate(b)
			audioBuf = append(audioBuf, samp)
		}
	}

	// Output to audio stream
	if err := outStream.WriteSamples(audioBuf); err != nil {
		return nil, fmt.Errorf("failed to write audio: %w", err)
	}

	// Free Memory back to pool
	bitstream.Reset()

	return &pb.TransmitResponse{
		Success: true,
		Message: "Transmission complete",
	}, nil
}

func (s *ModemServer) Receive(req *pb.ReceiveRequest, stream pb.ModemService_ReceiveServer) error {
	// Real implementation would attach to a Native Audio/TCP capture stream,
	// run the go-dsp Bandpass filters, and the `modem.PLLGardner` timing recovery.
	return fmt.Errorf("receive not implemented in this snippet")
}
