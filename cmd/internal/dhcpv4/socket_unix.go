//go:build !windows

package dhcpv4

import (
	"runtime"
	"syscall"
)

func (s *Server) listenerControl() func(string, string, syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error {
		var controlErr error
		err := c.Control(func(fd uintptr) {
			controlErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
			if controlErr != nil {
				return
			}
			_ = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			if s.InterfaceName != "" && runtime.GOOS == "linux" {
				controlErr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, s.InterfaceName)
			}
		})
		if err != nil {
			return err
		}
		return controlErr
	}
}
