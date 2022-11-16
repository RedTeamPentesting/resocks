# resocks

`resocks` is a reverse SOCKS5 proxy that can be used to route traffic through a
system that can't be directly accessed (e.g. due to NAT).

## Building

`resocks` can be built with the following command: `go build`

## Usage

**System A:** A system where the tools are deployed whose traffic should be routed through system B.

**System B:** The relay sytem through which the traffic should be routed.

1. Run `resocks listen` on system A (local process):
   - `resocks` will listen on port 4080 for connections of the remote `resocks` relay process
2. Run `resocks <IP of system A>` on system B (remote relay process)
   - The remote relay will connect to the local `resocks` server
   - The local `resocks` server will then open the SOCKS5 port 1080 on system A
3. Configure your tools on system A to use the SOCKS5 proxy on port 1080
   - The traffic will be routed through system B
