package store

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/zivkotp/zivko-dhcp/internal/model"
	"github.com/zivkotp/zivko-dhcp/internal/validation"
)

func TestFileRepositorySeedsMissingConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	repo, err := NewFileRepository(path)
	if err != nil {
		t.Fatalf("NewFileRepository() error = %v", err)
	}

	cfg, err := repo.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Pools) != 0 || len(cfg.Reservations) != 0 || len(cfg.Leases) != 0 {
		t.Fatal("expected missing config to be initialized empty")
	}
	if cfg.Runtime.ListenAddr != "" {
		t.Fatalf("expected empty default listen addr, got %q", cfg.Runtime.ListenAddr)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file to be created: %v", err)
	}
}

func TestFileRepositoryPersistsConfig(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	repo, err := NewFileRepository(path)
	if err != nil {
		t.Fatalf("NewFileRepository() error = %v", err)
	}

	cfg, err := repo.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	cfg.Pools = append(cfg.Pools, model.Pool{
		ID:     "pool-a",
		Name:   "Persisted Pool",
		Subnet: testCIDR(t, "192.168.10.0/24"),
		Range: model.IPv4Range{
			Start: testIP(t, "192.168.10.50"),
			End:   testIP(t, "192.168.10.180"),
		},
	})
	cfg.Reservations = append(cfg.Reservations, model.Reservation{
		ID:        "res-extra",
		PoolID:    "pool-a",
		Hostname:  "persisted-host",
		MAC:       "aa:bb:cc:dd:ee:ff",
		IPAddress: testIP(t, "192.168.10.11"),
	})

	if err := repo.Save(context.Background(), cfg); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	reloadedRepo, err := NewFileRepository(path)
	if err != nil {
		t.Fatalf("NewFileRepository() reload error = %v", err)
	}
	reloaded, err := reloadedRepo.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() reload error = %v", err)
	}

	if len(reloaded.Pools) != 1 {
		t.Fatalf("pool count = %d, want %d", len(reloaded.Pools), 1)
	}
	if got := reloaded.Pools[0].Name; got != "Persisted Pool" {
		t.Fatalf("reloaded pool name = %q, want %q", got, "Persisted Pool")
	}
	if len(reloaded.Reservations) != len(cfg.Reservations) {
		t.Fatalf("reservation count = %d, want %d", len(reloaded.Reservations), len(cfg.Reservations))
	}
}

func testIP(t *testing.T, raw string) net.IP {
	t.Helper()

	ip := net.ParseIP(raw)
	if ip == nil {
		t.Fatalf("invalid test IP %q", raw)
	}
	return ip
}

func testCIDR(t *testing.T, raw string) *net.IPNet {
	t.Helper()
	return validation.MustCIDR(raw)
}
