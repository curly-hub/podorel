package main

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPSRedirectURLUsesPublicURLAndRequestURI(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://curly-hub.local:9095/pods?state=running", nil)

	got := httpsRedirectURL("https://curly-hub.local:9095", req)
	want := "https://curly-hub.local:9095/pods?state=running"
	if got != want {
		t.Fatalf("redirect URL = %q, want %q", got, want)
	}
}

func TestProtocolMuxServesHTTPRedirectAndHTTPSOnSamePort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mux := newProtocolMux(listener, time.Second)
	errCh := make(chan error, 3)
	go func() {
		errCh <- mux.Serve(ctx)
	}()

	redirectServer := &http.Server{
		Handler:           httpsRedirectHandler("https://curly-hub.local:9095"),
		ReadHeaderTimeout: time.Second,
	}
	go func() {
		errCh <- redirectServer.Serve(mux.HTTPListener())
	}()

	seedServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	cert := seedServer.TLS.Certificates[0]
	seedServer.Close()

	appServer := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
		ReadHeaderTimeout: time.Second,
	}
	go func() {
		errCh <- appServer.Serve(tls.NewListener(mux.TLSListener(), &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		}))
	}()

	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
		defer shutdownCancel()
		_ = listener.Close()
		_ = redirectServer.Shutdown(shutdownCtx)
		_ = appServer.Shutdown(shutdownCtx)
	}()

	plainClient := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	httpResp, err := plainClient.Get("http://" + listener.Addr().String() + "/pods?state=running")
	if err != nil {
		t.Fatalf("plain HTTP request: %v", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("plain HTTP status = %d, want %d", httpResp.StatusCode, http.StatusPermanentRedirect)
	}
	if got, want := httpResp.Header.Get("Location"), "https://curly-hub.local:9095/pods?state=running"; got != want {
		t.Fatalf("redirect location = %q, want %q", got, want)
	}

	tlsClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	httpsResp, err := tlsClient.Get("https://" + listener.Addr().String() + "/api/health")
	if err != nil {
		t.Fatalf("HTTPS request: %v", err)
	}
	defer httpsResp.Body.Close()

	if httpsResp.StatusCode != http.StatusNoContent {
		t.Fatalf("HTTPS status = %d, want %d", httpsResp.StatusCode, http.StatusNoContent)
	}
}
