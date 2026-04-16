//go:build windows

package dhcpv4

import "syscall"

func (s *Server) listenerControl() func(string, string, syscall.RawConn) error {
	return nil
}
