// Package main implements resocks.
package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"

	"github.com/RedTeamPentesting/kbtls"

	"github.com/spf13/cobra"
)

var version = "build from source"

const (
	// DefaultProxyPort is the port on which the SOCKS5 server is exposed by default.
	DefaultProxyPort = 1080

	// DefaultListenPort is the port to which the reverse TLS connection is established by default.
	DefaultListenPort = 4080

	// ConnectionKeyEnvVariable is the environment variable through which the default connection key can be set.
	ConnectionKeyEnvVariable = "RESOCKS_KEY"
)

var (
	defaultConnectionKey      = ""
	defaultConnectBackAddress = ""
)

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
		Short: "Generates a connection key",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			key, err := kbtls.GenerateConnectionKey()
			if err != nil {
				return err
			}

			fmt.Println(key.String())

			return nil
		},
	}

	versionCmd := &cobra.Command{
		Use:   "version",
		Short: "Print the current version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("resocks %s\n", version)
		},
	}

	relayCmd.AddCommand(listenCmd)
	relayCmd.AddCommand(generateCmd)
	relayCmd.AddCommand(versionCmd)

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
	executable, err := os.Executable()
	if err == nil {
		return filepath.Base(executable)
	}

	if len(os.Args) > 0 {
		return filepath.Base(os.Args[0])
	}

	return "resocks"
}

func fromEnvWithFallback(envVariable string, fallback string) string {
	value, ok := os.LookupEnv(envVariable)
	if !ok {
		return fallback
	}

	return value
}
