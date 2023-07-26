package proxyrelay

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/hashicorp/yamux"
	"golang.org/x/sync/errgroup"
)

// RunProxy starts a SOCKS server on socks5listenAddr that tunnels all incoming
// connections through relayConn. The opposite site of the relayConn connection
// should be handled by RunRelay.
func RunProxy(ctx context.Context, relayConn net.Conn, socks5listenAddr string) (err error) {
	return RunProxyWithEventCallback(ctx, relayConn, socks5listenAddr, DefaultEventCallback)
}

// RunProxyWithEventCallback is like RunProxy but it allows to specify a custom
// event callback instead of DefaultEventCallback. If callback is nil, events
// are ignored.
func RunProxyWithEventCallback(
	ctx context.Context, relayConn net.Conn, socks5ListenAddr string, callback func(Event),
) error {
	if callback != nil {
		callback(Event{Type: TypeRelayConnected, Data: relayConn.RemoteAddr().String()})
	}

	err := handleRelayConnection(ctx, relayConn, socks5ListenAddr, callback)
	if errors.Is(err, net.ErrClosed) {
		err = nil
	}

	if callback != nil {
		data := ""
		if err != nil {
			data = err.Error()
		}

		callback(Event{Type: TypeRelayDisconnected, Data: data})
	}

	return err
}

func handleRelayConnection(ctx context.Context, relayConn net.Conn, proxyAddr string, callback func(Event)) error {
	go func() {
		<-ctx.Done()

		_ = relayConn.Close()
	}()

	client, err := yamux.Client(relayConn, yamuxCfg())
	if err != nil {
		return fmt.Errorf("initialize multiplexer: %w", err)
	}

	var tlsErr *tls.CertificateVerificationError

	// we use the first connection to receive socks-related errors from the relay
	errConn, err := client.Open()
	if err != nil {
		if errors.Is(err, yamux.ErrSessionShutdown) || errors.As(err, &tlsErr) {
			return fmt.Errorf("invalid connection key")
		}

		return fmt.Errorf("open error notification connection: %w", err)
	}

	// display the errors in the UI
	go handleErrorNotificationConnection(errConn, callback)

	err = startLocalProxyServer(proxyAddr, client, callback)
	if err != nil {
		return fmt.Errorf("proxy: %w", err)
	}

	return nil
}

func handleErrorNotificationConnection(conn net.Conn, callback func(Event)) {
	for {
		lengthBytes := make([]byte, 4)

		_, err := conn.Read(lengthBytes)
		if errors.Is(err, io.EOF) {
			return
		} else if err != nil {
			if callback != nil {
				callback(Event{
					Type: TypeError,
					Data: fmt.Sprintf("read message length from error notification connection: %v", err),
				})
			}

			return
		}

		msg := make([]byte, binary.BigEndian.Uint32(lengthBytes))

		_, err = conn.Read(msg)
		if err != nil {
			if callback != nil {
				callback(Event{Type: TypeError, Data: fmt.Sprintf("read message from error notification connection: %v", err)})
			}

			return
		}

		if callback != nil {
			callback(Event{Type: TypeError, Data: string(msg)})
		}
	}
}

func startLocalProxyServer(proxyAddr string, sess *yamux.Session, callback func(Event)) error {
	proxyListener, err := net.Listen("tcp", proxyAddr)
	if err != nil {
		return fmt.Errorf("listen for relay connection: %w", err)
	}

	defer proxyListener.Close() //nolint:errcheck

	if callback != nil {
		callback(Event{Type: TypeSOCKS5Active})
		defer callback(Event{Type: TypeSOCKS5Inactive})
	}

	var closedBecausePayloadDisconnected bool

	go func() {
		<-sess.CloseChan()

		closedBecausePayloadDisconnected = true

		err := proxyListener.Close()
		if err != nil && callback != nil {
			callback(Event{Type: TypeError, Data: fmt.Sprintf("socks5 close: %v", err)})
		}
	}()

	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		conn, err := proxyListener.Accept()
		if err != nil {
			if closedBecausePayloadDisconnected {
				return nil
			}

			return fmt.Errorf("accept socks5 connection: %w", err)
		}

		if callback != nil {
			callback(Event{Type: TypeSOCKS5ConnectionOpened, Data: formatAddr(conn.RemoteAddr())})
		}

		wg.Add(1)

		go func() {
			defer wg.Done()

			err := handleLocalProxyConn(conn, sess)
			if err != nil && callback != nil {
				callback(Event{Type: TypeError, Data: fmt.Sprintf("handling socks5 connection: %v", err)})
			}

			if callback != nil {
				callback(Event{Type: TypeSOCKS5ConnectionClosed, Data: formatAddr(conn.RemoteAddr())})
			}
		}()
	}
}

func handleLocalProxyConn(conn net.Conn, sess *yamux.Session) error {
	yamuxConn, err := sess.Open()
	if err != nil {
		return fmt.Errorf("open multiplexed connection: %w", err)
	}

	var eg errgroup.Group

	eg.Go(func() error {
		defer conn.Close()      //nolint:errcheck
		defer yamuxConn.Close() //nolint:errcheck

		_, err := io.Copy(yamuxConn, conn)
		if err != nil && !errors.Is(err, net.ErrClosed) {
			return fmt.Errorf("proxy->relay: %w", err)
		}

		return nil
	})

	eg.Go(func() error {
		defer conn.Close()      //nolint:errcheck
		defer yamuxConn.Close() //nolint:errcheck

		_, err := io.Copy(conn, yamuxConn)
		if err != nil && !errors.Is(err, net.ErrClosed) {
			return fmt.Errorf("relay->proxy: %w", err)
		}

		return nil
	})

	return eg.Wait()
}
