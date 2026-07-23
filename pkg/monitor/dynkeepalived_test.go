package monitor

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// startTLSServer starts a TLS server returning the given status code on
// /readyz and returns the port it listens on plus a cleanup function.
func startTLSServer(statusCode int) (uint16, func()) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/readyz" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(statusCode)
	}))
	_, portStr, err := net.SplitHostPort(srv.Listener.Addr().String())
	Expect(err).NotTo(HaveOccurred())
	port, err := strconv.ParseUint(portStr, 10, 16)
	Expect(err).NotTo(HaveOccurred())
	return uint16(port), srv.Close
}

var _ = Describe("isKubeApiHealthy", func() {
	It("reports healthy when /readyz returns 200", func() {
		port, cleanup := startTLSServer(http.StatusOK)
		defer cleanup()
		Expect(isKubeApiHealthy(port)).To(BeTrue())
	})

	It("reports unhealthy when /readyz returns 500", func() {
		port, cleanup := startTLSServer(http.StatusInternalServerError)
		defer cleanup()
		Expect(isKubeApiHealthy(port)).To(BeFalse())
	})

	It("reports unhealthy when the connection is refused", func() {
		// Grab a free port and close the listener so connections are refused.
		l, err := net.Listen("tcp", "localhost:0")
		Expect(err).NotTo(HaveOccurred())
		_, portStr, err := net.SplitHostPort(l.Addr().String())
		Expect(err).NotTo(HaveOccurred())
		Expect(l.Close()).To(Succeed())
		port, err := strconv.ParseUint(portStr, 10, 16)
		Expect(err).NotTo(HaveOccurred())
		Expect(isKubeApiHealthy(uint16(port))).To(BeFalse())
	})
})
