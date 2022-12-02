package proxyrelay

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"

	"github.com/armon/go-socks5"
	"github.com/hashicorp/yamux"
)

// RunRelay is the counterpart of RunProxy and acts as an exit node for the
// proxy connections tunneled through the provided connection.
func RunRelay(ctx context.Context, conn net.Conn) error {
	return RunRelayWithEventCallback(ctx, conn, DefaultEventCallback)
}

// RunRelayWithEventCallback is like RunRelay but it allows to specify a custom
// event callback instead of DefaultEventCallback. If callback is nil, events
// are ignored.
func RunRelayWithEventCallback(ctx context.Context, conn net.Conn, callback func(Event)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	yamuxServer, err := yamux.Server(conn, yamuxCfg())
	if err != nil {
		return fmt.Errorf("initialize multiplexer: %w", err)
	}

	go func() {
		<-ctx.Done()
		yamuxServer.Close() //nolint:errcheck,gosec
	}()

	defer yamuxServer.Close() //nolint:errcheck

	// we use the first connection to transfer socks-related errors to the listener
	errConn, err := yamuxServer.Accept()
	if err != nil {
		return fmt.Errorf("accept error notification connection: %w", err)
	}

	defer errConn.Close() //nolint:errcheck

	socksServer, err := socks5.New(&socks5.Config{Logger: newRemoteLogger(errConn, callback)})
	if err != nil {
		return fmt.Errorf("initialize socks5 server: %w", err)
	}

	err = socksServer.Serve(yamuxServer)
	if err != nil {
		return fmt.Errorf("socks5 server: %w", err)
	}

	return nil
}

func newRemoteLogger(conn net.Conn, callback func(Event)) *log.Logger {
	return log.New(&remoteLogger{Conn: conn, Callback: callback}, "", 0)
}

type remoteLogger struct {
	Conn     net.Conn
	Callback func(Event)
}

func (l *remoteLogger) Write(b []byte) (int, error) {
	msg := bytes.TrimPrefix(bytes.Trim(b, "\n"), []byte("[ERR] socks: "))

	if l.Callback != nil {
		l.Callback(Event{Type: TypeError, Data: "socks: " + string(msg)})
	}

	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(msg)))

	return l.Conn.Write(append(length, msg...)) //nolint:makezero
}
