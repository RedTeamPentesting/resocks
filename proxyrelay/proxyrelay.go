// Package proxyrelay implements both components of the relayed SOCKS5 proxy.
package proxyrelay

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"

	"github.com/hashicorp/yamux"
)

// Event holds the events that are generated by RunProxy and RunRelay.
type Event struct {
	Type string
	Data string
}

const (
	// TypeError signifies an error event with the error message stored in the Data attribute.
	TypeError = "error"

	// TypeRelayConnected is generated when RunProxy is started. The relay
	// connection's remote address is stored in the Data attribute.
	TypeRelayConnected = "relay connected"

	// TypeRelayDisconnected is generated when the relay connection is closed.
	// The Data attribute may contain a related error message.
	TypeRelayDisconnected = "relay disconnected"

	// TypeSOCKS5Active is generated when the SOCKS5 server is started. The Data
	// attribute is always empty.
	TypeSOCKS5Active = "SOCKS5 server active"

	// TypeSOCKS5Inactive is generated when the SOCKS5 server is stopped. The Data
	// attribute is always empty.
	TypeSOCKS5Inactive = "SOCKS5 server inactive"

	// TypeSOCKS5ConnectionOpened is generate whenever a new connection is opened
	// through the SOCKS5 server. The IP of host that initiated the connection
	// is stored in the Data attribute.
	TypeSOCKS5ConnectionOpened = "SOCKS5 connection opened"

	// TypeSOCKS5ConnectionClosed is generate whenever a connection through the
	// SOCKS5 server is closed. The IP of host that initiated the connection is
	// stored in the Data attribute.
	TypeSOCKS5ConnectionClosed = "SOCKS5 connection closed"
)

// DefaultEventCallback prints all events to stdout except for error events,
// which are printed to stderr. SOCKS5ConnectionOpened and
// SOCKS5ConnectionClosed events are ignored.
var DefaultEventCallback = func(e Event) {
	switch e.Type {
	case TypeError:
		fmt.Fprintf(os.Stderr, "error: %s\n", e.Data)
	case TypeRelayConnected:
		fmt.Printf("relay %s connected\n", e.Data)
	case TypeRelayDisconnected:
		fmt.Print("relay disconnected")
		if e.Data != "" {
			fmt.Print(": " + e.Data)
		}

		fmt.Println()
	case TypeSOCKS5Active:
		fmt.Println("SOCKS5 server active")
	case TypeSOCKS5Inactive:
		fmt.Println("SOCKS5 server inactive")
	case TypeSOCKS5ConnectionOpened, TypeSOCKS5ConnectionClosed:
		// ignore
	default:
		fmt.Fprintf(os.Stderr, "unexpected event %q: %s\n", e.Type, e.Data)
	}
}

func yamuxCfg() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = nil
	cfg.Logger = log.New(io.Discard, "", 0)

	return cfg
}

func formatAddr(addr net.Addr) string {
	switch addr := addr.(type) {
	case *net.UDPAddr:
		return addr.IP.String()
	case *net.TCPAddr:
		return addr.IP.String()
	default:
		return "unknown"
	}
}
