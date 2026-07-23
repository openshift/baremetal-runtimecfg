package monitor

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

// startTLSServer starts a TLS server returning the given status code and
// returns the port it listens on plus a cleanup function.
func startTLSServer(t *testing.T, statusCode int) (uint16, func()) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(statusCode)
	}))
	_, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to parse test server address: %v", err)
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		t.Fatalf("failed to parse test server port: %v", err)
	}
	return uint16(port), srv.Close
}

func TestIsKubeApiHealthyReturns200(t *testing.T) {
	port, cleanup := startTLSServer(t, http.StatusOK)
	defer cleanup()
	if !isKubeApiHealthy(port) {
		t.Error("expected healthy for /readyz returning 200")
	}
}

func TestIsKubeApiHealthyReturns500(t *testing.T) {
	port, cleanup := startTLSServer(t, http.StatusInternalServerError)
	defer cleanup()
	if isKubeApiHealthy(port) {
		t.Error("expected unhealthy for /readyz returning 500")
	}
}

func TestIsKubeApiHealthyConnectionRefused(t *testing.T) {
	// Grab a free port and close the listener so connections are refused.
	l, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	_, portStr, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatalf("failed to parse address: %v", err)
	}
	l.Close()
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		t.Fatalf("failed to parse port: %v", err)
	}
	if isKubeApiHealthy(uint16(port)) {
		t.Error("expected unhealthy when connection is refused")
	}
}
