package proxyrelay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"

	"golang.org/x/sync/errgroup"
)

const (
	testServerAddr = "127.0.0.222:222"
	testProxyAddr  = "127.0.0.111:111"
)

func TestProxyRelay(t *testing.T) { //nolint:cyclop
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	proxyReady := make(chan struct{})
	receivedRelayConnectedEvent := false
	receivedRelayDisconnectedEvent := false
	receivedSOCKS5ActiveEvent := false
	receivedSOCKS5InactiveEvent := false
	receivedSOCKS5ConnectionOpenedEvent := false

	// at the very end, check if we have received all events
	defer func() {
		if !receivedRelayConnectedEvent {
			t.Fatalf("did not receive relay connected event")
		}

		if !receivedRelayDisconnectedEvent {
			t.Errorf("did not receive relay disconnected event")
		}

		if !receivedSOCKS5ActiveEvent {
			t.Errorf("did not receive SOCKS5 active event")
		}

		if !receivedSOCKS5InactiveEvent {
			t.Errorf("did not receive SOCKS5 inactive event")
		}

		if !receivedSOCKS5ConnectionOpenedEvent {
			t.Errorf("did not receive SOCKS5 connection opened event")
		}
	}()

	pipeA, pipeB := net.Pipe()
	defer pipeA.Close() //nolint:errcheck
	defer pipeB.Close() //nolint:errcheck

	var eg errgroup.Group //nolint:varnamelen

	eg.Go(func() error {
		return RunRelay(pipeA)
	})

	eg.Go(func() error {
		return RunProxyWithEventCallback(ctx, pipeB, testProxyAddr, func(e Event) {
			switch e.Type {
			case TypeRelayConnected:
				receivedRelayConnectedEvent = true
			case TypeRelayDisconnected:
				receivedRelayDisconnectedEvent = true
			case TypeSOCKS5Active:
				receivedSOCKS5ActiveEvent = true
				close(proxyReady)
			case TypeSOCKS5Inactive:
				receivedSOCKS5InactiveEvent = true
			case TypeSOCKS5ConnectionOpened:
				receivedSOCKS5ConnectionOpenedEvent = true
			}
		})
	})

	body := []byte("success")

	//nolint:gosec
	server := http.Server{
		Addr: testServerAddr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(body) //nolint:errcheck
		}),
	}

	defer server.Shutdown(context.Background()) //nolint:errcheck

	eg.Go(server.ListenAndServe)

	proxyURL, err := url.Parse("socks5://" + testProxyAddr)
	if err != nil {
		t.Fatalf("parse proxy URL: %v", err)
	}

	<-proxyReady

	http.DefaultClient.Transport = &http.Transport{Proxy: http.ProxyURL(proxyURL)}

	res, err := http.Get("http://" + testServerAddr) //nolint:noctx
	if err != nil {
		t.Fatalf("send request through proxy: %v", err)
	}

	receivedBody, err := io.ReadAll(res.Body)
	if err != nil {
		res.Body.Close() //nolint:errcheck,gosec
		t.Fatalf("read response body: %v", err)
	}

	err = res.Body.Close()
	if err != nil {
		t.Fatalf("close response body: %v", err)
	}

	if !bytes.Equal(receivedBody, body) {
		t.Fatalf("received %q instead of %q", receivedBody, body)
	}

	pipeA.Close()                         //nolint:errcheck,gosec
	pipeB.Close()                         //nolint:errcheck,gosec
	server.Shutdown(context.Background()) //nolint:errcheck,gosec
	cancel()

	err = eg.Wait()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatal(err)
	}
}
