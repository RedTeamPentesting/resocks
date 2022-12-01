package proxyrelay

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"

	"github.com/armon/go-socks5"
	"github.com/hashicorp/yamux"
)

// RunRelay is the counterpart of RunProxy and acts as an exit node for the
// proxy connections tunneled through the provided connection.
func RunRelay(conn net.Conn) error {
	yamuxServer, err := yamux.Server(conn, yamuxCfg())
	if err != nil {
		return fmt.Errorf("initialize multiplexer: %w", err)
	}

	defer yamuxServer.Close() //nolint:errcheck

	// we use the first connection to transfer socks-related errors to the listener
	errConn, err := yamuxServer.Accept()
	if err != nil {
		return fmt.Errorf("accept error notification connection: %w", err)
	}

	defer errConn.Close() //nolint:errcheck

	socksServer, err := socks5.New(&socks5.Config{Logger: newRemoteLogger(errConn)})
	if err != nil {
		return fmt.Errorf("initialize socks5 server: %w", err)
	}

	err = socksServer.Serve(yamuxServer)
	if err != nil {
		return fmt.Errorf("socks5 server: %w", err)
	}

	return nil
}

type remoteLogger struct {
	conn net.Conn
}

func (l *remoteLogger) Write(b []byte) (int, error) {
	n, err := os.Stderr.Write(b)
	if err != nil {
		return n, err
	}

	msg := bytes.TrimPrefix(bytes.Trim(b, "\n"), []byte("[ERR] socks: "))
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(msg)))

	return l.conn.Write(append(length, msg...)) //nolint:makezero
}

func newRemoteLogger(conn net.Conn) *log.Logger {
	return log.New(&remoteLogger{conn}, "", 0)
}
