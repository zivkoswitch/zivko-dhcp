package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/zivkotp/zivko-dhcp/internal/model"
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
	if len(cfg.Pools) == 0 {
		t.Fatal("expected seeded pools")
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

	cfg.Pools[0].Name = "Persisted Pool"
	cfg.Reservations = append(cfg.Reservations, model.Reservation{
		ID:        "res-extra",
		PoolID:    cfg.Pools[0].ID,
		Hostname:  "persisted-host",
		MAC:       "aa:bb:cc:dd:ee:ff",
		IPAddress: mustIP("192.168.10.11"),
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

	if got := reloaded.Pools[0].Name; got != "Persisted Pool" {
		t.Fatalf("reloaded pool name = %q, want %q", got, "Persisted Pool")
	}
	if len(reloaded.Reservations) != len(cfg.Reservations) {
		t.Fatalf("reservation count = %d, want %d", len(reloaded.Reservations), len(cfg.Reservations))
	}
}
