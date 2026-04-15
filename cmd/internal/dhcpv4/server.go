package dhcpv4

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"runtime"
	"syscall"
	"time"

	"github.com/parallels/dhcp-gui/internal/model"
	"github.com/parallels/dhcp-gui/internal/store"
)

type Server struct {
	Addr          string
	ServerIP      net.IP
	InterfaceName string
	Repository    store.Repository
	Allocator     *Allocator
	Logger        *log.Logger
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	if s.Repository == nil {
		return fmt.Errorf("repository is required")
	}
	addr := s.Addr
	if addr == "" {
		addr = ":67"
	}
	pc, err := s.listenPacket(addr)
	if err != nil {
		return err
	}
	defer pc.Close()

	go func() {
		<-ctx.Done()
		_ = pc.Close()
	}()

	buffer := make([]byte, 1500)
	for {
		n, remoteAddr, err := pc.ReadFrom(buffer)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		if err := s.handlePacket(pc, remoteAddr, buffer[:n]); err != nil && s.Logger != nil {
			s.Logger.Printf("dhcp handle error: %v", err)
		}
	}
}

func (s *Server) listenPacket(addr string) (net.PacketConn, error) {
	listenConfig := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
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
		},
	}
	return listenConfig.ListenPacket(context.Background(), "udp4", addr)
}

func (s *Server) handlePacket(pc net.PacketConn, remoteAddr net.Addr, payload []byte) error {
	packet, err := ParsePacket(payload)
	if err != nil {
		return err
	}

	switch packet.MessageType() {
	case MessageDiscover, MessageRequest:
		return s.handleAllocate(pc, remoteAddr, packet)
	case MessageRelease:
		return s.handleRelease(packet)
	default:
		return nil
	}
}

func (s *Server) handleAllocate(pc net.PacketConn, remoteAddr net.Addr, packet Packet) error {
	cfg, err := s.Repository.Load(context.Background())
	if err != nil {
		return err
	}
	now := time.Now()
	cfg.Leases = PruneExpiredLeases(cfg.Leases, now)

	allocator := s.Allocator
	if allocator == nil {
		allocator = &Allocator{Now: func() time.Time { return now }}
	}
	serverIP := effectiveServerIP(s.ServerIP, model.Pool{})

	if packet.MessageType() == MessageRequest {
		if requestServerID := packet.ServerIdentifier(); requestServerID != nil && serverIP != nil && !requestServerID.Equal(serverIP) {
			return nil
		}
	}

	allocation, err := allocator.Allocate(cfg, AllocationRequest{
		MAC:           packet.CHAddr,
		RequestedIP:   packet.RequestedIP(),
		Hostname:      packet.Hostname(),
		ClientID:      packet.ClientID(),
		ServerIP:      s.ServerIP,
		LeaseDuration: time.Duration(packet.LeaseDuration()) * time.Second,
		VendorClassID: packet.VendorClassID(),
	})
	if err != nil {
		if packet.MessageType() == MessageRequest {
			reply := NewReply(packet, MessageNak, serverIP, nil)
			return s.writeReply(pc, remoteAddr, reply)
		}
		return nil
	}

	cfg.Leases = upsertLease(cfg.Leases, allocation.Lease)
	if err := s.Repository.Save(context.Background(), cfg); err != nil {
		return err
	}

	replyType := byte(MessageOffer)
	if packet.MessageType() == MessageRequest {
		replyType = byte(MessageAck)
	}
	replyServerIP := effectiveServerIP(s.ServerIP, allocation.Pool)
	reply := NewReply(packet, replyType, replyServerIP, allocation.Lease.IPAddress)
	reply.Options[OptionIPAddressLeaseTime] = make([]byte, 4)
	binary.BigEndian.PutUint32(reply.Options[OptionIPAddressLeaseTime], uint32(allocation.Lease.Duration/time.Second))
	reply.Options[OptionRenewalTimeValue] = make([]byte, 4)
	binary.BigEndian.PutUint32(reply.Options[OptionRenewalTimeValue], uint32((allocation.Lease.Duration/2)/time.Second))
	reply.Options[OptionRebindingTimeValue] = make([]byte, 4)
	binary.BigEndian.PutUint32(reply.Options[OptionRebindingTimeValue], uint32(((allocation.Lease.Duration*7)/8)/time.Second))
	if allocation.Pool.Subnet != nil {
		reply.Options[OptionSubnetMask] = []byte(allocation.Pool.Subnet.Mask)
	}
	if allocation.Pool.DefaultGateway != nil {
		reply.Options[OptionRouter] = allocation.Pool.DefaultGateway.To4()
	}
	if len(allocation.Pool.DNSServers) > 0 {
		reply.Options[OptionDNSServer] = flattenIPs(allocation.Pool.DNSServers)
	}
	if allocation.Pool.DomainName != "" {
		reply.Options[OptionDomainName] = []byte(allocation.Pool.DomainName)
	}
	return s.writeReply(pc, remoteAddr, reply)
}

func (s *Server) handleRelease(packet Packet) error {
	cfg, err := s.Repository.Load(context.Background())
	if err != nil {
		return err
	}
	cfg.Leases = removeLease(cfg.Leases, packet.CHAddr, packet.CIAddr)
	return s.Repository.Save(context.Background(), cfg)
}

func (s *Server) writeReply(pc net.PacketConn, remoteAddr net.Addr, reply Packet) error {
	data, err := reply.MarshalBinary()
	if err != nil {
		return err
	}
	target := remoteAddr
	if udpAddr, ok := remoteAddr.(*net.UDPAddr); ok && (udpAddr.IP == nil || udpAddr.IP.Equal(net.IPv4zero) || reply.Flags&0x8000 != 0) {
		target = &net.UDPAddr{IP: net.IPv4bcast, Port: 68}
	}
	_, err = pc.WriteTo(data, target)
	return err
}

func upsertLease(leases []model.Lease, lease model.Lease) []model.Lease {
	filtered := make([]model.Lease, 0, len(leases)+1)
	for _, existing := range leases {
		if existing.MAC == lease.MAC || (existing.IPAddress != nil && lease.IPAddress != nil && existing.IPAddress.Equal(lease.IPAddress)) {
			continue
		}
		filtered = append(filtered, existing)
	}
	return append(filtered, lease)
}

func removeLease(leases []model.Lease, mac net.HardwareAddr, ip net.IP) []model.Lease {
	normalizedMAC := normalizeMAC(mac)
	filtered := make([]model.Lease, 0, len(leases))
	for _, lease := range leases {
		if normalizedMAC != "" && lease.MAC == normalizedMAC {
			continue
		}
		if ip != nil && lease.IPAddress != nil && lease.IPAddress.Equal(ip) {
			continue
		}
		filtered = append(filtered, lease)
	}
	return filtered
}

func flattenIPs(ips []net.IP) []byte {
	out := make([]byte, 0, len(ips)*4)
	for _, ip := range ips {
		if v4 := ip.To4(); v4 != nil {
			out = append(out, v4...)
		}
	}
	return out
}

func effectiveServerIP(serverIP net.IP, pool model.Pool) net.IP {
	if serverIP != nil {
		return serverIP
	}
	if pool.DefaultGateway != nil {
		return pool.DefaultGateway
	}
	return net.IPv4(0, 0, 0, 0)
}
