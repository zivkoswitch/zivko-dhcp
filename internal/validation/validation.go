package validation

import (
	"fmt"
	"net"
	"sort"

	"github.com/zivkotp/zivko-dhcp/internal/ipcalc"
	"github.com/zivkotp/zivko-dhcp/internal/model"
)

func ValidateConfig(cfg model.Config) error {
	if err := ValidatePools(cfg.Pools); err != nil {
		return err
	}
	if err := ValidateExclusions(cfg.Pools, cfg.Exclusions); err != nil {
		return err
	}
	if err := ValidateReservations(cfg.Pools, cfg.Reservations); err != nil {
		return err
	}
	return nil
}

func ValidatePools(pools []model.Pool) error {
	sorted := append([]model.Pool(nil), pools...)
	sort.Slice(sorted, func(i, j int) bool {
		return ipcalc.CompareIP(sorted[i].Range.Start, sorted[j].Range.Start) < 0
	})

	for i, pool := range sorted {
		normRange, err := ipcalc.NormalizeRange(pool.Range)
		if err != nil {
			return fmt.Errorf("pool %q has invalid range: %w", pool.Name, err)
		}
		if pool.Subnet == nil {
			return fmt.Errorf("pool %q requires a subnet", pool.Name)
		}
		if !pool.Subnet.Contains(normRange.Start) || !pool.Subnet.Contains(normRange.End) {
			return fmt.Errorf("pool %q range must stay inside subnet", pool.Name)
		}
		if pool.DefaultGateway != nil && !pool.Subnet.Contains(pool.DefaultGateway) {
			return fmt.Errorf("pool %q default gateway must stay inside subnet", pool.Name)
		}
		for _, dnsServer := range pool.DNSServers {
			if dnsServer == nil {
				return fmt.Errorf("pool %q has an invalid dns server entry", pool.Name)
			}
		}
		sorted[i].Range = normRange
		if i > 0 && ipcalc.Overlaps(sorted[i-1].Range, sorted[i].Range) {
			return fmt.Errorf("pool %q overlaps pool %q", sorted[i-1].Name, sorted[i].Name)
		}
	}

	return nil
}

func ValidateExclusions(pools []model.Pool, exclusions []model.Exclusion) error {
	poolByID := make(map[string]model.Pool, len(pools))
	for _, pool := range pools {
		normRange, err := ipcalc.NormalizeRange(pool.Range)
		if err != nil {
			return fmt.Errorf("pool %q has invalid range: %w", pool.Name, err)
		}
		pool.Range = normRange
		poolByID[pool.ID] = pool
	}

	perPool := make(map[string][]model.IPv4Range)
	for _, exclusion := range exclusions {
		pool, ok := poolByID[exclusion.PoolID]
		if !ok {
			return fmt.Errorf("exclusion %q references unknown pool", exclusion.ID)
		}
		rng, err := ipcalc.NormalizeRange(exclusion.Range)
		if err != nil {
			return fmt.Errorf("exclusion %q has invalid range: %w", exclusion.ID, err)
		}
		if !ipcalc.ContainsRange(pool.Range, rng) {
			return fmt.Errorf("exclusion %q must stay inside pool %q", exclusion.ID, pool.Name)
		}
		perPool[pool.ID] = append(perPool[pool.ID], rng)
	}

	for poolID, ranges := range perPool {
		ipcalc.SortRanges(ranges)
		for i := 1; i < len(ranges); i++ {
			if ipcalc.Overlaps(ranges[i-1], ranges[i]) {
				return fmt.Errorf("exclusions overlap in pool %q", poolID)
			}
		}
	}

	return nil
}

func ValidateReservations(pools []model.Pool, reservations []model.Reservation) error {
	poolByID := make(map[string]model.Pool, len(pools))
	for _, pool := range pools {
		if pool.Subnet == nil {
			return fmt.Errorf("pool %q requires a subnet", pool.Name)
		}
		poolByID[pool.ID] = pool
	}

	usedIPs := map[string]model.Reservation{}
	for _, reservation := range reservations {
		pool, ok := poolByID[reservation.PoolID]
		if !ok {
			return fmt.Errorf("reservation %q references unknown pool", reservation.ID)
		}
		if reservation.MAC == "" && reservation.Hostname == "" {
			return fmt.Errorf("reservation %q requires a mac address or hostname", reservation.ID)
		}
		ip, err := ipcalc.NormalizeIPv4(reservation.IPAddress)
		if err != nil {
			return fmt.Errorf("reservation %q has invalid ip: %w", reservation.ID, err)
		}
		if !pool.Subnet.Contains(ip) {
			return fmt.Errorf("reservation %q ip must stay inside pool subnet", reservation.ID)
		}

		key := ip.String()
		if existing, exists := usedIPs[key]; exists {
			return fmt.Errorf("reservation %q collides with reservation %q", reservation.ID, existing.ID)
		}
		usedIPs[key] = reservation
	}

	return nil
}

func MustCIDR(raw string) *net.IPNet {
	_, network, err := net.ParseCIDR(raw)
	if err != nil {
		panic(err)
	}
	return network
}
