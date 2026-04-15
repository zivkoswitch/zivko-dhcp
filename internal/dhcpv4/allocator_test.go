package dhcpv4

import (
	"net"
	"testing"
	"time"

	"github.com/parallels/dhcp-gui/internal/model"
	"github.com/parallels/dhcp-gui/internal/validation"
)

func TestAllocatorPrefersReservation(t *testing.T) {
	t.Parallel()

	cfg := model.Config{
		Pools: []model.Pool{
			{
				ID:     "pool-a",
				Name:   "A",
				Subnet: validation.MustCIDR("192.168.1.0/24"),
				Range:  model.IPv4Range{Start: net.ParseIP("192.168.1.10"), End: net.ParseIP("192.168.1.20")},
			},
		},
		Reservations: []model.Reservation{
			{
				ID:        "r1",
				PoolID:    "pool-a",
				MAC:       "aa:bb:cc:dd:ee:ff",
				IPAddress: net.ParseIP("192.168.1.99"),
			},
		},
	}

	allocation, err := (&Allocator{}).Allocate(cfg, AllocationRequest{
		MAC: net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
	})
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	if got := allocation.Lease.IPAddress.String(); got != "192.168.1.99" {
		t.Fatalf("allocated ip = %s, want 192.168.1.99", got)
	}
}

func TestAllocatorSkipsActiveLeaseAndExclusions(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	cfg := model.Config{
		Pools: []model.Pool{
			{
				ID:     "pool-a",
				Name:   "A",
				Subnet: validation.MustCIDR("10.0.0.0/24"),
				Range:  model.IPv4Range{Start: net.ParseIP("10.0.0.10"), End: net.ParseIP("10.0.0.14")},
			},
		},
		Exclusions: []model.Exclusion{
			{
				ID:     "e1",
				PoolID: "pool-a",
				Range:  model.IPv4Range{Start: net.ParseIP("10.0.0.10"), End: net.ParseIP("10.0.0.11")},
			},
		},
		Leases: []model.Lease{
			{
				ID:        "l1",
				PoolID:    "pool-a",
				MAC:       "11:22:33:44:55:66",
				IPAddress: net.ParseIP("10.0.0.12"),
				ExpiresAt: now.Add(1 * time.Hour),
			},
		},
	}

	allocation, err := (&Allocator{Now: func() time.Time { return now }}).Allocate(cfg, AllocationRequest{
		MAC: net.HardwareAddr{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01},
	})
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	if got := allocation.Lease.IPAddress.String(); got != "10.0.0.13" {
		t.Fatalf("allocated ip = %s, want 10.0.0.13", got)
	}
}

func TestAllocatorRenewsExistingLeaseForSameMAC(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	cfg := model.Config{
		Pools: []model.Pool{
			{
				ID:     "pool-a",
				Name:   "A",
				Subnet: validation.MustCIDR("10.1.0.0/24"),
				Range:  model.IPv4Range{Start: net.ParseIP("10.1.0.10"), End: net.ParseIP("10.1.0.20")},
			},
		},
		Leases: []model.Lease{
			{
				ID:        "l1",
				PoolID:    "pool-a",
				MAC:       "aa:bb:cc:dd:ee:ff",
				IPAddress: net.ParseIP("10.1.0.15"),
				ExpiresAt: now.Add(30 * time.Minute),
			},
		},
	}

	allocation, err := (&Allocator{Now: func() time.Time { return now }}).Allocate(cfg, AllocationRequest{
		MAC: net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff},
	})
	if err != nil {
		t.Fatalf("Allocate() error = %v", err)
	}
	if got := allocation.Lease.IPAddress.String(); got != "10.1.0.15" {
		t.Fatalf("allocated ip = %s, want 10.1.0.15", got)
	}
	if !allocation.Renewed {
		t.Fatal("expected renewed allocation")
	}
}

func TestPruneExpiredLeases(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	leases := []model.Lease{
		{ID: "expired", ExpiresAt: now.Add(-1 * time.Minute)},
		{ID: "active", ExpiresAt: now.Add(5 * time.Minute)},
	}
	got := PruneExpiredLeases(leases, now)
	if len(got) != 1 || got[0].ID != "active" {
		t.Fatalf("got leases = %#v", got)
	}
}
