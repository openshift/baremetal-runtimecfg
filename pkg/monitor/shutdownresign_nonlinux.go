//go:build !linux

package monitor

import (
	"io"
	"net"
)

// handleShutdownResign is only supported on Linux; on other platforms it is
// a no-op so the package still compiles.
func handleShutdownResign(conn io.Writer, vips []net.IP) {
}
