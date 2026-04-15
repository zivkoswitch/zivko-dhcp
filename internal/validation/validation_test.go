package validation

import (
	"net"
	"testing"

	"github.com/parallels/dhcp-gui/internal/model"
)

func ip(raw string) net.IP {
	return net.ParseIP(raw)
}

func TestValidateConfigAcceptsValidData(t *testing.T) {
	t.Parallel()

	cfg := model.Config{
		Pools: []model.Pool{
			{
				ID:     "pool-a",
				Name:   "Primary",
				Subnet: MustCIDR("192.168.1.0/24"),
				Range: model.IPv4Range{
					Start: ip("192.168.1.10"),
					End:   ip("192.168.1.100"),
				},
			},
		},
		Exclusions: []model.Exclusion{
			{
				ID:     "ex-1",
				PoolID: "pool-a",
				Range: model.IPv4Range{
					Start: ip("192.168.1.20"),
					End:   ip("192.168.1.25"),
				},
			},
		},
		Reservations: []model.Reservation{
			{
				ID:        "res-1",
				PoolID:    "pool-a",
				Hostname:  "printer",
				MAC:       "52:54:00:12:34:56",
				IPAddress: ip("192.168.1.200"),
			},
		},
	}

	if err := ValidateConfig(cfg); err != nil {
		t.Fatalf("ValidateConfig() error = %v", err)
	}
}

func TestValidatePoolsRejectsOverlap(t *testing.T) {
	t.Parallel()

	pools := []model.Pool{
		{
			ID:     "a",
			Name:   "A",
			Subnet: MustCIDR("10.0.0.0/24"),
			Range:  model.IPv4Range{Start: ip("10.0.0.10"), End: ip("10.0.0.50")},
		},
		{
			ID:     "b",
			Name:   "B",
			Subnet: MustCIDR("10.0.0.0/24"),
			Range:  model.IPv4Range{Start: ip("10.0.0.40"), End: ip("10.0.0.80")},
		},
	}

	if err := ValidatePools(pools); err == nil {
		t.Fatal("expected overlap error")
	}
}

func TestValidateReservationsRejectsDuplicateIPs(t *testing.T) {
	t.Parallel()

	pools := []model.Pool{
		{
			ID:     "a",
			Name:   "A",
			Subnet: MustCIDR("10.0.0.0/24"),
			Range:  model.IPv4Range{Start: ip("10.0.0.10"), End: ip("10.0.0.50")},
		},
	}
	reservations := []model.Reservation{
		{ID: "r1", PoolID: "a", IPAddress: ip("10.0.0.60")},
		{ID: "r2", PoolID: "a", IPAddress: ip("10.0.0.60")},
	}

	if err := ValidateReservations(pools, reservations); err == nil {
		t.Fatal("expected duplicate reservation error")
	}
}
