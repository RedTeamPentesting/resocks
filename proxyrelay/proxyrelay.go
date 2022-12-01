// Package proxyrelay implements both components of the relayed SOCKS5 proxy.
package proxyrelay

import (
	"io"
	"log"

	"github.com/hashicorp/yamux"
)

func yamuxCfg() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = nil
	cfg.Logger = log.New(io.Discard, "", 0)

	return cfg
}
