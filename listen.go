package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"

	"github.com/RedTeamPentesting/resocks/proxyrelay"

	"github.com/RedTeamPentesting/kbtls"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func listenCommand() *cobra.Command {
	listenAddr := withDefaultPort("", DefaultListenPort)
	proxyAddr := withDefaultPort("localhost", DefaultProxyPort)
	abortOnDisconnect := false
	connectionKey := fromEnvWithFallback(ConnectionKeyEnvVariable, defaultConnectionKey)
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
		fmt.Sprintf("Configures a static connection key instead of generating a key "+
			"(default can be set using environment variable %s)",
			ConnectionKeyEnvVariable))
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

	updateUI, waitForUItoShutdown, uiShutdown := startUI(key, connectBackAddr, proxyAddr, insecure, noColor)
	defer func() {
		// if the proxy terminates autonomously, notify UI and wait for it to close aswell
		updateUI(shutdownMessage{})
		<-uiShutdown // only terminate after UI has cleaned up the terminal
	}()

	go func() {
		uiErr := waitForUItoShutdown()

		cancel() // notify proxy to shutdown after UI is terminated (ctrl+c)

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

	return proxyrelay.RunProxyWithEventCallback(ctx, relayConn, proxyAddr, func(e proxyrelay.Event) {
		if e.Type != proxyrelay.TypeSOCKS5ConnectionOpened && e.Type != proxyrelay.TypeSOCKS5ConnectionClosed {
			updateUI(e)
		}
	})
}

func serverTLSConfig(connectionKey string, insecure bool) (*tls.Config, kbtls.ConnectionKey, error) {
	var (
		key kbtls.ConnectionKey
		err error
	)

	if connectionKey != "" {
		key, err = kbtls.ParseConnectionKey(connectionKey)
		if err != nil {
			return nil, key, fmt.Errorf("parse connection key: %w", err)
		}
	} else {
		key, err = kbtls.GenerateConnectionKey()
		if err != nil {
			return nil, key, fmt.Errorf("generate connection key: %w", err)
		}
	}

	cfg, err := kbtls.ServerTLSConfig(key)
	if err != nil {
		return nil, key, fmt.Errorf("configure TLS: %w", err)
	}

	if insecure {
		cfg.ClientAuth = tls.NoClientCert
		cfg.ClientCAs = nil
	}

	return cfg, key, nil
}
