// Package main implements resocks.
package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/hashicorp/yamux"
	"github.com/spf13/cobra"
)

const (
	// DefaultProxyPort is the port on which the SOCKS5 server is exposed by default.
	DefaultProxyPort = 1080
	// DefaultListenPort is the port to which the reverse TLS connection is established by default.
	DefaultListenPort = 4080
	// ConnectionKeyEnvVariable is the environment variable through which the default connection key can be set.
	ConnectionKeyEnvVariable = "RESOCKS_KEY"
)

var defaultConnectionKey = ""

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)

		os.Exit(1)
	}
}

func run() error {
	relayCmd := relayCommand()
	listenCmd := listenCommand()

	generateCmd := &cobra.Command{
		Use:   "generate",
		Short: "Generates a connection ID",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			key, err := GenerateConnectionKey()
			if err != nil {
				return err
			}

			fmt.Println(key.String())

			return nil
		},
	}

	relayCmd.AddCommand(listenCmd)
	relayCmd.AddCommand(generateCmd)

	return relayCmd.Execute()
}

func withDefaultPort(addr string, defaultPort int) string {
	_, _, err := net.SplitHostPort(addr)
	if err == nil {
		return addr
	}

	return addr + ":" + strconv.Itoa(defaultPort)
}

func binaryName() string {
	if len(os.Args) > 0 {
		return filepath.Base(os.Args[0])
	}

	return "resocks"
}

func yamuxCfg() *yamux.Config {
	cfg := yamux.DefaultConfig()
	cfg.LogOutput = nil
	cfg.Logger = log.New(io.Discard, "", 0)

	return cfg
}

func fromEnvIfEmpty(value string, envVariable string) string {
	if value != "" {
		return value
	}

	return os.Getenv(envVariable)
}
