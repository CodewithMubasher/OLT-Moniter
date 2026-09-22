package monitor

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProbeTargetHTTPURL(t *testing.T) {
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	success, _, err := probeTarget(context.Background(), server.URL+"/health", time.Second)
	if err != nil || !success {
		t.Fatalf("HTTP target should be reachable: success=%v err=%v", success, err)
	}
	if requestedPath != "/health" {
		t.Fatalf("requested path = %q, want /health", requestedPath)
	}
}

func TestProbeTargetHTTPResponseIsReachable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	success, _, err := probeTarget(context.Background(), server.URL, time.Second)
	if err != nil || !success {
		t.Fatalf("an HTTP 404 still proves the server is reachable: success=%v err=%v", success, err)
	}
}

func TestProbeTargetHostPortUsesTCP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	success, _, err := probeTarget(context.Background(), listener.Addr().String(), time.Second)
	if err != nil || !success {
		t.Fatalf("TCP target should be reachable: success=%v err=%v", success, err)
	}
}
