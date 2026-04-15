package dhcpv4

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/parallels/dhcp-gui/internal/ipcalc"
	"github.com/parallels/dhcp-gui/internal/model"
	"github.com/parallels/dhcp-gui/internal/validation"
)

const DefaultLeaseDuration = 12 * time.Hour

type Allocator struct {
	Now func() time.Time
}

type AllocationRequest struct {
	MAC             net.HardwareAddr
	RequestedIP     net.IP
	Hostname        string
	ClientID        string
	ServerIP        net.IP
	LeaseDuration   time.Duration
	VendorClassID   string
	ParameterFields []byte
}

type Allocation struct {
	Pool      model.Pool
	Lease     model.Lease
	Requested bool
	Reserved  bool
	Renewed   bool
}

func (a *Allocator) Allocate(cfg model.Config, req AllocationRequest) (Allocation, error) {
	if err := validation.ValidateConfig(cfg); err != nil {
		return Allocation{}, err
	}
	if len(req.MAC) == 0 {
		return Allocation{}, fmt.Errorf("mac address is required")
	}

	now := time.Now()
	if a != nil && a.Now != nil {
		now = a.Now()
	}
	leaseDuration := req.LeaseDuration
	if leaseDuration <= 0 {
		leaseDuration = DefaultLeaseDuration
	}

	pool, ok := pickPool(cfg, req.RequestedIP)
	if !ok {
		return Allocation{}, fmt.Errorf("no pool available for request")
	}

	if reservation, found := reservationForMACOrHostname(cfg, pool.ID, req.MAC, req.Hostname); found {
		lease := buildLease(pool.ID, reservation.IPAddress, req, now, leaseDuration)
		return Allocation{Pool: pool, Lease: lease, Reserved: true, Requested: req.RequestedIP != nil}, nil
	}

	if existing, found := activeLeaseForMAC(cfg, pool.ID, req.MAC, now); found && usableRequestedLease(pool, existing, req.RequestedIP) {
		lease := buildLease(pool.ID, existing.IPAddress, req, now, leaseDuration)
		return Allocation{Pool: pool, Lease: lease, Renewed: true, Requested: req.RequestedIP != nil}, nil
	}

	if req.RequestedIP != nil {
		if usableInPool(cfg, pool, req.RequestedIP, now) {
			lease := buildLease(pool.ID, req.RequestedIP, req, now, leaseDuration)
			return Allocation{Pool: pool, Lease: lease, Requested: true}, nil
		}
	}

	effective, err := effectivePoolRanges(cfg, pool.ID, pool.Range)
	if err != nil {
		return Allocation{}, err
	}
	for _, rng := range effective {
		for ipValue := ipcalc.IPToUint32(rng.Start); ipValue <= ipcalc.IPToUint32(rng.End); ipValue++ {
			candidate := ipcalc.Uint32ToIP(ipValue)
			if !usableInPool(cfg, pool, candidate, now) {
				continue
			}
			lease := buildLease(pool.ID, candidate, req, now, leaseDuration)
			return Allocation{Pool: pool, Lease: lease}, nil
		}
	}

	return Allocation{}, fmt.Errorf("no available lease in pool %q", pool.Name)
}

func buildLease(poolID string, ip net.IP, req AllocationRequest, now time.Time, duration time.Duration) model.Lease {
	idHash := sha1.Sum([]byte(poolID + "|" + normalizeMAC(req.MAC) + "|" + ip.String()))
	return model.Lease{
		ID:         "lease-" + hex.EncodeToString(idHash[:8]),
		PoolID:     poolID,
		Hostname:   strings.TrimSpace(req.Hostname),
		MAC:        normalizeMAC(req.MAC),
		IPAddress:  ip,
		ExpiresAt:  now.Add(duration),
		Duration:   duration,
		Vendor:     strings.TrimSpace(req.VendorClassID),
		ClientID:   strings.TrimSpace(req.ClientID),
		LastSeenAt: now,
	}
}

func pickPool(cfg model.Config, requestedIP net.IP) (model.Pool, bool) {
	if requestedIP != nil {
		for _, pool := range cfg.Pools {
			if pool.Subnet != nil && pool.Subnet.Contains(requestedIP) {
				return pool, true
			}
		}
	}
	if len(cfg.Pools) == 0 {
		return model.Pool{}, false
	}
	return cfg.Pools[0], true
}

func reservationForMACOrHostname(cfg model.Config, poolID string, mac net.HardwareAddr, hostname string) (model.Reservation, bool) {
	normalizedMAC := normalizeMAC(mac)
	normalizedHost := strings.TrimSpace(hostname)
	for _, reservation := range cfg.Reservations {
		if reservation.PoolID != poolID {
			continue
		}
		if normalizedMAC != "" && strings.EqualFold(reservation.MAC, normalizedMAC) {
			return reservation, true
		}
		if normalizedHost != "" && reservation.Hostname == normalizedHost {
			return reservation, true
		}
	}
	return model.Reservation{}, false
}

func activeLeaseForMAC(cfg model.Config, poolID string, mac net.HardwareAddr, now time.Time) (model.Lease, bool) {
	normalizedMAC := normalizeMAC(mac)
	for _, lease := range cfg.Leases {
		if lease.PoolID != poolID || lease.MAC != normalizedMAC {
			continue
		}
		if lease.ExpiresAt.After(now) {
			return lease, true
		}
	}
	return model.Lease{}, false
}

func effectivePoolRanges(cfg model.Config, poolID string, poolRange model.IPv4Range) ([]model.IPv4Range, error) {
	exclusions := make([]model.IPv4Range, 0)
	for _, exclusion := range cfg.Exclusions {
		if exclusion.PoolID == poolID {
			exclusions = append(exclusions, exclusion.Range)
		}
	}
	return ipcalc.EffectiveRanges(poolRange, exclusions)
}

func usableInPool(cfg model.Config, pool model.Pool, ip net.IP, now time.Time) bool {
	if pool.Subnet == nil || !pool.Subnet.Contains(ip) || !ipcalc.ContainsIP(pool.Range, ip) {
		return false
	}
	for _, reservation := range cfg.Reservations {
		if reservation.PoolID == pool.ID && reservation.IPAddress != nil && reservation.IPAddress.Equal(ip) {
			return false
		}
	}
	for _, lease := range cfg.Leases {
		if lease.PoolID == pool.ID && lease.IPAddress != nil && lease.IPAddress.Equal(ip) && lease.ExpiresAt.After(now) {
			return false
		}
	}
	return true
}

func usableRequestedLease(pool model.Pool, lease model.Lease, requestedIP net.IP) bool {
	if lease.IPAddress == nil {
		return false
	}
	if pool.Subnet == nil || !pool.Subnet.Contains(lease.IPAddress) || !ipcalc.ContainsIP(pool.Range, lease.IPAddress) {
		return false
	}
	if requestedIP != nil && !lease.IPAddress.Equal(requestedIP) {
		return false
	}
	return true
}

func PruneExpiredLeases(leases []model.Lease, now time.Time) []model.Lease {
	filtered := make([]model.Lease, 0, len(leases))
	for _, lease := range leases {
		if lease.ExpiresAt.IsZero() || lease.ExpiresAt.After(now) {
			filtered = append(filtered, lease)
		}
	}
	return filtered
}

func normalizeMAC(mac net.HardwareAddr) string {
	if len(mac) == 0 {
		return ""
	}
	return strings.ToLower(mac.String())
}
