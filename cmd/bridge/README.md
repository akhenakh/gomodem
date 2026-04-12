```
+-----------------------------------------------------------------+
|                    Guest OS (VM / Container)                    |
|                                                                 |
|  +-------------------------+       +-------------------------+  |
|  |     User Applications   |       |   Guest Network Stack   |  |
|  |  (curl, ping, iperf3)   |<----->|  (TCP/UDP, IPv4, ICMP)  |  |
|  +-------------------------+       +-------------------------+  |
|                                                 |               |
|    + - - - - - - - - - - - - - - - - - - - - - -|- - - - - - +  |
|    | Virtual Interface (eth0)[ MTU: 256 ]     v            |  |
|    | (Linux kernel automatically fragments large IP packets) |  |
|    + - - - - - - - - - - - - - - - - - - - - - - - - - - - - +  |
+-------------------------------------------------|---------------+
                                                  |[ vsock / virtio / unix socket / tcp transport ]
                     (Passes raw IP/Ethernet frames)
                                                  |
+=================================================|===============+
|                   Custom Go Gateway Application                 |
|                                                 |               |
|  +----------------------------------------------|------------+  |
|  |                   tap.Switch                 v            |  |
|  |       (gvisor-tap-vsock Virtual Layer 2 Switch)           |  |
|  +----------------------------------------------|------------+  |
|                                                 |               |
|  +----------------------------------------------|------------+  |
|  |                AX25RadioDevice               v            |  |
|  |  (Our Custom tap.VirtualDevice implementation)            |  |
|  |                                                           |  |
|  |  +-----------------------------------------------------+  |  |
|  |  | 1. Protocol Filter (Drops MTU > 256, Keeps IPv4)    |  |  |
|  |  +-----------------------------------------------------+  |  |
|  |                              | (IPv4 Packet Bytes)        |  |
|  |  +---------------------------v-------------------------+  |  |
|  |  | 2. AX.25 Framer (gomodem protocols package)         |  |  |
|  |  |    * Prepends PID 0xCC (IPv4 routing indicator)     |  |  |
|  |  |    * Wraps in UI Frame, Calculates CRC/FCS          |  |  |
|  |  |    * Applies Bit-stuffing & NRZI Encoding           |  |  |
|  |  +-----------------------------------------------------+  |  |
|  |                              | (NRZI Bitstream)           |  |
|  |  +---------------------------v-------------------------+  |  |
|  |  | 3. DSP Modem (gomodem modem package)                |  |  |
|  |  |    * DDS AFSK Modulator (1200 / 2200 Hz Tones)      |  |  |
|  |  |    * SDFT Demodulator & Gardner PLL (for RX)        |  |  |
|  |  +-----------------------------------------------------+  |  |
|  +------------------------------|----------------------------+  |
+=================================|===============================+
                                  | 
                         (64-bit float PCM Audio)
                                  |
+---------------------------------v-------------------------------+
|                      Host Audio Interface                       |
|        (ALSA / PulseAudio / PortAudio / TCP Audio Pipe)         |
+---------------------------------|-------------------------------+
                                  |
                       (Analog Audio In / Out)
                        (PTT / VOX Signaling)
                                  |
+---------------------------------v-------------------------------+
|                    VHF/UHF Radio Transceiver                    |
|             (Half-Duplex, ~250ms TX/RX Switch Delay)            |
+---------------------------------|-------------------------------+
                                  |
               ~ ~ ~ ~ ~ ~  [ RF Airwaves ]  ~ ~ ~ ~ ~ ~  
```
