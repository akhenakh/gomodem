# gomodem

gomodem is a software-defined modem service providing gRPC-based modulation and demodulation for packet radio.

The modem library is fully functional, the tools are work in progress.

## Technical Overview

The project implements a digital signal processing (DSP) pipeline for converting bitstreams to audio (modulation) and audio back to bitstreams (demodulation), targeting amateur radio packet standards.

It can be used as a library or as a server.

### Modulation
- **AFSK**: Direct Digital Synthesis (DDS) AFSK modulation with optional transition smoothing.
- **GFSK**: Gaussian Frequency Shift Keying using a Gaussian lowpass FIR filter (G3RUH standard, sometimes called GMSK but it is really just GFSK because the modulation index is not exactly 0.5.)
- **Sine FSK**: Half-cosine transition modulation.
- **Supported Baud Rates**: 300, 1200, 2400, 9600.

### Demodulation
- **Filtering**: Biquad IIR bandpass filter for signal isolation.
- **Detection**: Sliding DFT (SDFT) for low-latency frequency discrimination.
- **Timing Recovery**: Gardner Phase-Locked Loop (PLL) for symbol boundary synchronization.

### Protocols
- **AX.25**: Full implementation of the AX.25 link-layer protocol for packet radio.

## API

The `ModemService` gRPC interface provides:

- `Transmit`: Encodes a payload using the specified protocol and modulates it to a target (WAV file or TCP stream).
- `Receive`: Streams demodulated packets from a specified audio source.

## Project Structure

- `/cmd/modemd`: gRPC server entry point.
- `/cmd/atest`: CLI tool to decode AX.25 frames from WAV files (similar to Direwolf `atest`).
- `/modem`: Core DSP implementations (modulators, demodulators, filters).
- `/protocols`: Protocol encoding/decoding logic (e.g., AX.25).
- `/server`: gRPC service implementation.
- `/audio`: Audio stream abstractions for various backends.
- `/proto`: Protobuf definitions for the service API.

## Usage

### Running the Server
```bash
go run cmd/modemd/main.go
```
The server listens on port `50051` by default.


## Testing against Direwolf

```sh
go test -v ./...
cd testdata
sox ../modem/test.wav -t raw -r 44100 -e signed-integer -b 16 -c 1 - \
  | direwolf -r 44100 -n 1 -b 16 -
```
