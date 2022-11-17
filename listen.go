package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/hashicorp/yamux"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func listenCommand() *cobra.Command {
	listenAddr := withDefaultPort("", DefaultListenPort)
	proxyAddr := withDefaultPort("localhost", DefaultProxyPort)
	abortOnDisconnect := false
	connectionKey := fromEnvIfEmpty(defaultConnectionKey, ConnectionKeyEnvVariable)
	insecure := false
	noColor := false

	listenCmd := &cobra.Command{
		Use:   "listen",
		Short: "Listen for reverse connections from the relay process and start the SOCKS5 server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLocalSocksProxy(
				withDefaultPort(listenAddr, DefaultListenPort),
				withDefaultPort(proxyAddr, DefaultProxyPort),
				abortOnDisconnect,
				connectionKey,
				insecure,
				noColor,
			)
		},
	}

	listenFlags := listenCmd.Flags()
	listenFlags.StringVar(&listenAddr, "on", listenAddr,
		"Address to listen on for reverse connections")
	listenFlags.StringVarP(&proxyAddr, "proxy-address", "p", proxyAddr,
		"Address for the socks proxy server")
	listenFlags.BoolVar(&abortOnDisconnect, "abort-on-disconnect", false, "Abort when the relay disconnects")
	listenFlags.StringVarP(&connectionKey, "key", "k", connectionKey,
		"Configures a static connection key instead of generating a key")
	listenFlags.BoolVar(&insecure, "insecure", insecure, "Disables client certificate validation")
	listenFlags.BoolVar(&noColor, "no-color", noColor, "Disables colored output")

	return listenCmd
}

func runLocalSocksProxy(
	connectBackAddr string, proxyAddr string, abortOnDisconnect bool,
	connectionKey string, insecure bool, noColor bool,
) (err error) {
	tlsConfig, key, err := serverTLSConfig(connectionKey, insecure)
	if err != nil {
		return fmt.Errorf("build TLS config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	updateUI, waitForUItoShutdown := startUI(key, connectBackAddr, proxyAddr, insecure, noColor)
	defer updateUI(statusShutdown)

	go func() {
		uiErr := waitForUItoShutdown()

		cancel()

		if err == nil || errors.Is(err, net.ErrClosed) {
			err = uiErr
		}
	}()

	listener, err := tls.Listen("tcp", connectBackAddr, tlsConfig)
	if err != nil {
		return fmt.Errorf("listen for relay connection: %w", err)
	}

	go func() {
		<-ctx.Done()

		_ = listener.Close()
	}()

	defer listener.Close() //nolint:errcheck

	for {
		err = handleRelayConnection(ctx, listener, proxyAddr, updateUI)
		if errors.Is(err, net.ErrClosed) {
			err = nil
		}

		updateUI(relayDisconnectedMessage(err))

		if abortOnDisconnect || ctx.Err() != nil {
			return nil
		}
	}
}

func handleRelayConnection(ctx context.Context, listener net.Listener, proxyAddr string, updateUI func(tea.Msg)) error {
	relayConn, err := listener.Accept()
	if err != nil {
		return fmt.Errorf("accept relay connection: %w", err)
	}

	go func() {
		<-ctx.Done()

		_ = relayConn.Close()
	}()

	defer relayConn.Close() //nolint:errcheck

	updateUI(relayConnectedMessage(asIP(relayConn.RemoteAddr())))

	client, err := yamux.Client(relayConn, yamuxCfg())
	if err != nil {
		return fmt.Errorf("initialize multiplexer: %w", err)
	}

	err = startLocalProxyServer(proxyAddr, client, updateUI)
	if err != nil {
		return fmt.Errorf("proxy: %w", err)
	}

	return nil
}

func startLocalProxyServer(proxyAddr string, sess *yamux.Session, updateUI func(tea.Msg)) error {
	proxyListener, err := net.Listen("tcp", proxyAddr)
	if err != nil {
		return fmt.Errorf("listen for relay connection: %w", err)
	}

	defer proxyListener.Close() //nolint:errcheck

	updateUI(statusSOCKSActive)
	defer updateUI(statusSOCKSInactive)

	var closedBecausePayloadDisconnected bool

	go func() {
		<-sess.CloseChan()

		closedBecausePayloadDisconnected = true

		err := proxyListener.Close()
		if err != nil {
			updateUI(fmt.Errorf("socks5 close: %w", err))
		}
	}()

	for {
		conn, err := proxyListener.Accept()
		if err != nil {
			if closedBecausePayloadDisconnected {
				return nil
			}

			return fmt.Errorf("accept socks5 connection: %w", err)
		}

		go func() {
			err := handleLocalProxyConn(conn, sess)
			if err != nil {
				updateUI(fmt.Errorf("handling socks5 connection: %w", err))
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
		_, err := io.Copy(yamuxConn, conn)
		if err != nil {
			return fmt.Errorf("proxy->relay: %w", err)
		}

		return nil
	})

	eg.Go(func() error {
		_, err := io.Copy(conn, yamuxConn)
		if err != nil {
			return fmt.Errorf("relay->proxy: %w", err)
		}

		return nil
	})

	return eg.Wait()
}

func serverTLSConfig(connectionKey string, insecure bool) (*tls.Config, ConnectionKey, error) {
	var (
		key ConnectionKey
		err error
	)

	if connectionKey != "" {
		key, err = ParseConnectionKey(connectionKey)
		if err != nil {
			return nil, key, fmt.Errorf("parse connection key: %w", err)
		}
	} else {
		key, err = GenerateConnectionKey()
		if err != nil {
			return nil, key, fmt.Errorf("generate connection key: %w", err)
		}
	}

	cfg, err := ServerTLSConfig(key)
	if err != nil {
		return nil, key, fmt.Errorf("configure TLS: %w", err)
	}

	if insecure {
		cfg.ClientAuth = tls.NoClientCert
		cfg.ClientCAs = nil
	}

	return cfg, key, nil
}

func asIP(addr net.Addr) net.IP {
	switch a := addr.(type) {
	case *net.TCPAddr:
		return a.IP
	case *net.UDPAddr:
		return a.IP
	default:
		panic(fmt.Sprintf("unexpected address type: %T", a))
	}
}
