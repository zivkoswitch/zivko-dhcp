package configgen

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/parallels/dhcp-gui/internal/ipcalc"
	"github.com/parallels/dhcp-gui/internal/model"
)

func Render(cfg model.Config) (string, error) {
	var lines []string
	lines = append(lines, "# Native Go DHCP Server Preview")
	lines = append(lines, fmt.Sprintf("Pools: %d | Exclusions: %d | Reservations: %d | Active Leases: %d", len(cfg.Pools), len(cfg.Exclusions), len(cfg.Reservations), len(cfg.Leases)))
	lines = append(lines, "")

	pools := append([]model.Pool(nil), cfg.Pools...)
	sort.Slice(pools, func(i, j int) bool {
		return ipcalc.CompareIP(pools[i].Range.Start, pools[j].Range.Start) < 0
	})

	exclusionsByPool := make(map[string][]model.IPv4Range)
	for _, exclusion := range cfg.Exclusions {
		exclusionsByPool[exclusion.PoolID] = append(exclusionsByPool[exclusion.PoolID], exclusion.Range)
	}

	for _, pool := range pools {
		lines = append(lines, fmt.Sprintf("[Pool] %s", pool.Name))
		if pool.Subnet != nil {
			lines = append(lines, fmt.Sprintf("  subnet: %s", pool.Subnet.String()))
		}
		lines = append(lines, fmt.Sprintf("  configured range: %s - %s", pool.Range.Start, pool.Range.End))
		effective, err := ipcalc.EffectiveRanges(pool.Range, exclusionsByPool[pool.ID])
		if err != nil {
			return "", fmt.Errorf("pool %q: %w", pool.Name, err)
		}
		if len(effective) == 0 {
			lines = append(lines, "  effective ranges: none")
		} else {
			lines = append(lines, "  effective ranges:")
			for _, rng := range effective {
				lines = append(lines, fmt.Sprintf("    - %s - %s", rng.Start, rng.End))
			}
		}
		if pool.DefaultGateway != nil {
			lines = append(lines, fmt.Sprintf("  default gateway: %s", pool.DefaultGateway))
		}
		if len(pool.DNSServers) > 0 {
			lines = append(lines, fmt.Sprintf("  dns servers: %s", joinIPs(pool.DNSServers)))
		}
		if pool.DomainName != "" {
			lines = append(lines, fmt.Sprintf("  domain name: %s", pool.DomainName))
		}
		lines = append(lines, "")
	}

	reservations := append([]model.Reservation(nil), cfg.Reservations...)
	sort.Slice(reservations, func(i, j int) bool {
		return reservations[i].IPAddress.String() < reservations[j].IPAddress.String()
	})
	if len(reservations) > 0 {
		lines = append(lines, "[Reservations]")
	}
	for _, reservation := range reservations {
		parts := make([]string, 0, 4)
		if reservation.PoolID != "" {
			parts = append(parts, "pool="+reservation.PoolID)
		}
		if reservation.MAC != "" {
			parts = append(parts, "mac="+reservation.MAC)
		}
		if reservation.Hostname != "" {
			parts = append(parts, "hostname="+reservation.Hostname)
		}
		parts = append(parts, "ip="+reservation.IPAddress.String())
		lines = append(lines, "  - "+strings.Join(parts, " | "))
	}

	return strings.Join(lines, "\n"), nil
}

func joinIPs(ips []net.IP) string {
	parts := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip != nil {
			parts = append(parts, ip.String())
		}
	}
	return strings.Join(parts, ",")
}
