If you want to integrate directly with the Host OS, you use a **TUN (Network Tunnel)** interface. 

### How the TUN Interface Works
A TUN device is a virtual network interface (like `tun0`). To the Host OS, it looks exactly like a physical hardware adapter. The Linux/macOS/Windows kernel handles all the complex TCP/IP stack logic. 
However, instead of transmitting packets down an Ethernet cable, the kernel hands the **raw IPv4 packets** directly to a file descriptor in your Go application.

### Architecture
```text
+-------------------------------------------------------+
|                    Host OS Kernel                     |
|  (Native TCP/IP Stack, Routing Tables, Applications)  |
+---------------------------|---------------------------+
                            |[ ip route add 44.0.0.0/8 dev tun0 ][ ip link set dev tun0 mtu 256     ]
                            |
+---------------------------v---------------------------+
|                      tun0 Device                      |
|                (Layer 3 - Raw IP Packets)             |
+---------------------------|---------------------------+
                            |
+===========================|===========================+
|                      gomodem (Go)                     |
|                                                       |
|  1. Read bytes from tun0                              |
|  2. Prepend AX.25 PID (0xCC for IPv4)                 |
|  3. AX.25 Frame Encode & NRZI Bitstuffing             |
|  4. Modulate to AFSK Audio (1200 / 2200 Hz)           |
+===========================|===========================+
                            |
                       (Audio Out)
```

### Running and Configuring the Host Route

Because creating a network interface modifies the host kernel, you **must run the compiled Go binary as root**.

1. Start your bridge:
   ```bash
   go build -o radio-tnc
   sudo ./radio-tnc
   ```

2. Open a second terminal and configure the interface that your Go program just created (typically `tun0`):
   ```bash
   # Assign your host an IP address on the radio network
   sudo ip addr add 10.0.0.1/24 dev tun0
   
   # IMPORTANT: Force the host to chunk packets to survive 1200 baud physics
   sudo ip link set dev tun0 mtu 256 up
   
   # Route all traffic destined for the AMPRNet (44.x.x.x) through the radio
   sudo ip route add 44.0.0.0/8 dev tun0
   ```

Now, if you type `ping 44.1.2.3` in your terminal, the Linux kernel will format the ICMP request, hand it to your Go program via `tun0`, and you'll see your `gomodem` print `[TX] Host OS sent IPv4 packet... Modulating to audio...`!

*(Note: We use TUN [Layer 3 - IP] instead of TAP [Layer 2 - Ethernet] because AX.25 does not use Ethernet MAC addresses or ARP broadcasts. Passing raw IP packets directly via TUN is exactly how traditional Linux `kissattach` works).*
