package store

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/zivkotp/zivko-dhcp/internal/model"
	"github.com/zivkotp/zivko-dhcp/internal/validation"
)

type Repository interface {
	Load(context.Context) (model.Config, error)
	Save(context.Context, model.Config) error
}

type FileRepository struct {
	mu   sync.RWMutex
	path string
	cfg  model.Config
}

func (r *FileRepository) Path() string {
	return r.path
}

type configFile struct {
	Runtime      runtimeFile       `json:"runtime"`
	Pools        []poolFile        `json:"pools"`
	Exclusions   []exclusionFile   `json:"exclusions"`
	Reservations []reservationFile `json:"reservations"`
	Leases       []leaseFile       `json:"leases"`
}

type runtimeFile struct {
	ListenAddr    string `json:"listen_addr,omitempty"`
	ServerIP      string `json:"server_ip,omitempty"`
	InterfaceName string `json:"interface_name,omitempty"`
	ControlSocket string `json:"control_socket,omitempty"`
}

type poolFile struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	SubnetCIDR     string   `json:"subnet_cidr"`
	RangeStart     string   `json:"range_start"`
	RangeEnd       string   `json:"range_end"`
	DefaultGateway string   `json:"default_gateway,omitempty"`
	DNSServers     []string `json:"dns_servers,omitempty"`
	DomainName     string   `json:"domain_name,omitempty"`
}

type exclusionFile struct {
	ID         string `json:"id"`
	PoolID     string `json:"pool_id"`
	RangeStart string `json:"range_start"`
	RangeEnd   string `json:"range_end"`
}

type reservationFile struct {
	ID        string `json:"id"`
	PoolID    string `json:"pool_id"`
	Hostname  string `json:"hostname"`
	MAC       string `json:"mac"`
	IPAddress string `json:"ip_address"`
}

type leaseFile struct {
	ID         string `json:"id"`
	PoolID     string `json:"pool_id"`
	Hostname   string `json:"hostname"`
	MAC        string `json:"mac"`
	IPAddress  string `json:"ip_address"`
	ExpiresAt  string `json:"expires_at"`
	Duration   string `json:"duration"`
	Vendor     string `json:"vendor"`
	ClientID   string `json:"client_id"`
	LastSeenAt string `json:"last_seen_at"`
}

func NewFileRepository(path string) (*FileRepository, error) {
	if path == "" {
		defaultPath, err := DefaultConfigPath()
		if err != nil {
			return nil, err
		}
		path = defaultPath
	}

	return &FileRepository{
		path: path,
		cfg:  seedConfig(),
	}, nil
}

func DefaultConfigPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(configDir, "zivko-dhcp", "config.json"), nil
}

func (r *FileRepository) Load(_ context.Context) (model.Config, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			if err := r.writeLocked(r.cfg); err != nil {
				return model.Config{}, err
			}
			return r.cfg, nil
		}
		return model.Config{}, fmt.Errorf("read config %s: %w", r.path, err)
	}

	cfg, err := decodeConfig(data)
	if err != nil {
		return model.Config{}, fmt.Errorf("decode config %s: %w", r.path, err)
	}
	r.cfg = cfg
	return cfg, nil
}

func (r *FileRepository) Save(_ context.Context, cfg model.Config) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.writeLocked(cfg)
}

func (r *FileRepository) writeLocked(cfg model.Config) error {
	data, err := encodeConfig(cfg)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	tmpPath := r.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := os.Rename(tmpPath, r.path); err != nil {
		return fmt.Errorf("replace config file: %w", err)
	}

	r.cfg = cfg
	return nil
}

func encodeConfig(cfg model.Config) ([]byte, error) {
	out := configFile{
		Runtime: runtimeFile{
			ListenAddr:    cfg.Runtime.ListenAddr,
			ServerIP:      cfg.Runtime.ServerIP,
			InterfaceName: cfg.Runtime.InterfaceName,
			ControlSocket: cfg.Runtime.ControlSocket,
		},
		Pools:        make([]poolFile, 0, len(cfg.Pools)),
		Exclusions:   make([]exclusionFile, 0, len(cfg.Exclusions)),
		Reservations: make([]reservationFile, 0, len(cfg.Reservations)),
		Leases:       make([]leaseFile, 0, len(cfg.Leases)),
	}

	for _, pool := range cfg.Pools {
		subnet := ""
		if pool.Subnet != nil {
			subnet = pool.Subnet.String()
		}
		out.Pools = append(out.Pools, poolFile{
			ID:             pool.ID,
			Name:           pool.Name,
			SubnetCIDR:     subnet,
			RangeStart:     ipString(pool.Range.Start),
			RangeEnd:       ipString(pool.Range.End),
			DefaultGateway: ipString(pool.DefaultGateway),
			DNSServers:     ipStrings(pool.DNSServers),
			DomainName:     pool.DomainName,
		})
	}

	for _, exclusion := range cfg.Exclusions {
		out.Exclusions = append(out.Exclusions, exclusionFile{
			ID:         exclusion.ID,
			PoolID:     exclusion.PoolID,
			RangeStart: ipString(exclusion.Range.Start),
			RangeEnd:   ipString(exclusion.Range.End),
		})
	}

	for _, reservation := range cfg.Reservations {
		out.Reservations = append(out.Reservations, reservationFile{
			ID:        reservation.ID,
			PoolID:    reservation.PoolID,
			Hostname:  reservation.Hostname,
			MAC:       reservation.MAC,
			IPAddress: ipString(reservation.IPAddress),
		})
	}

	for _, lease := range cfg.Leases {
		out.Leases = append(out.Leases, leaseFile{
			ID:         lease.ID,
			PoolID:     lease.PoolID,
			Hostname:   lease.Hostname,
			MAC:        lease.MAC,
			IPAddress:  ipString(lease.IPAddress),
			ExpiresAt:  lease.ExpiresAt.Format(time.RFC3339),
			Duration:   lease.Duration.String(),
			Vendor:     lease.Vendor,
			ClientID:   lease.ClientID,
			LastSeenAt: lease.LastSeenAt.Format(time.RFC3339),
		})
	}

	return json.MarshalIndent(out, "", "  ")
}

func MarshalConfig(cfg model.Config) ([]byte, error) {
	return encodeConfig(cfg)
}

func decodeConfig(data []byte) (model.Config, error) {
	var raw configFile
	if err := json.Unmarshal(data, &raw); err != nil {
		return model.Config{}, err
	}

	cfg := model.Config{
		Runtime: model.RuntimeSettings{
			ListenAddr:    raw.Runtime.ListenAddr,
			ServerIP:      raw.Runtime.ServerIP,
			InterfaceName: raw.Runtime.InterfaceName,
			ControlSocket: raw.Runtime.ControlSocket,
		},
		Pools:        make([]model.Pool, 0, len(raw.Pools)),
		Exclusions:   make([]model.Exclusion, 0, len(raw.Exclusions)),
		Reservations: make([]model.Reservation, 0, len(raw.Reservations)),
		Leases:       make([]model.Lease, 0, len(raw.Leases)),
	}

	for _, pool := range raw.Pools {
		var subnet *net.IPNet
		if pool.SubnetCIDR != "" {
			_, parsed, err := net.ParseCIDR(pool.SubnetCIDR)
			if err != nil {
				return model.Config{}, fmt.Errorf("invalid subnet for pool %q: %w", pool.ID, err)
			}
			subnet = parsed
		}
		cfg.Pools = append(cfg.Pools, model.Pool{
			ID:             pool.ID,
			Name:           pool.Name,
			Subnet:         subnet,
			DefaultGateway: parseIP(pool.DefaultGateway),
			DNSServers:     parseIPs(pool.DNSServers),
			DomainName:     pool.DomainName,
			Range: model.IPv4Range{
				Start: parseIP(pool.RangeStart),
				End:   parseIP(pool.RangeEnd),
			},
		})
	}

	for _, exclusion := range raw.Exclusions {
		cfg.Exclusions = append(cfg.Exclusions, model.Exclusion{
			ID:     exclusion.ID,
			PoolID: exclusion.PoolID,
			Range: model.IPv4Range{
				Start: parseIP(exclusion.RangeStart),
				End:   parseIP(exclusion.RangeEnd),
			},
		})
	}

	for _, reservation := range raw.Reservations {
		cfg.Reservations = append(cfg.Reservations, model.Reservation{
			ID:        reservation.ID,
			PoolID:    reservation.PoolID,
			Hostname:  reservation.Hostname,
			MAC:       reservation.MAC,
			IPAddress: parseIP(reservation.IPAddress),
		})
	}

	for _, lease := range raw.Leases {
		expiresAt, err := parseTime(lease.ExpiresAt)
		if err != nil {
			return model.Config{}, fmt.Errorf("invalid lease expiry for %q: %w", lease.ID, err)
		}
		lastSeenAt, err := parseTime(lease.LastSeenAt)
		if err != nil {
			return model.Config{}, fmt.Errorf("invalid lease last seen for %q: %w", lease.ID, err)
		}
		duration, err := time.ParseDuration(lease.Duration)
		if err != nil {
			return model.Config{}, fmt.Errorf("invalid lease duration for %q: %w", lease.ID, err)
		}

		cfg.Leases = append(cfg.Leases, model.Lease{
			ID:         lease.ID,
			PoolID:     lease.PoolID,
			Hostname:   lease.Hostname,
			MAC:        lease.MAC,
			IPAddress:  parseIP(lease.IPAddress),
			ExpiresAt:  expiresAt,
			Duration:   duration,
			Vendor:     lease.Vendor,
			ClientID:   lease.ClientID,
			LastSeenAt: lastSeenAt,
		})
	}

	return cfg, nil
}

func UnmarshalConfig(data []byte) (model.Config, error) {
	return decodeConfig(data)
}

func parseIP(raw string) net.IP {
	if raw == "" {
		return nil
	}
	return net.ParseIP(raw)
}

func parseTime(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339, raw)
}

func ipString(ip net.IP) string {
	if ip == nil {
		return ""
	}
	return ip.String()
}

func ipStrings(ips []net.IP) []string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		if ip != nil {
			out = append(out, ip.String())
		}
	}
	return out
}

func parseIPs(raw []string) []net.IP {
	out := make([]net.IP, 0, len(raw))
	for _, item := range raw {
		ip := parseIP(item)
		if ip != nil {
			out = append(out, ip)
		}
	}
	return out
}

func seedConfig() model.Config {
	return model.Config{
		Runtime: model.RuntimeSettings{
			ListenAddr:    ":6767",
			ServerIP:      "0.0.0.0",
			InterfaceName: "",
			ControlSocket: "",
		},
		Pools: []model.Pool{
			{
				ID:             "pool-lan",
				Name:           "LAN",
				Subnet:         mustCIDR("192.168.10.0/24"),
				DefaultGateway: mustIP("192.168.10.1"),
				DNSServers:     []net.IP{mustIP("192.168.10.1"), mustIP("1.1.1.1")},
				DomainName:     "lan.example.internal",
				Range: model.IPv4Range{
					Start: mustIP("192.168.10.50"),
					End:   mustIP("192.168.10.180"),
				},
			},
			{
				ID:             "pool-lab",
				Name:           "Lab",
				Subnet:         mustCIDR("192.168.20.0/24"),
				DefaultGateway: mustIP("192.168.20.1"),
				DNSServers:     []net.IP{mustIP("192.168.20.1"), mustIP("8.8.8.8")},
				DomainName:     "lab.example.internal",
				Range: model.IPv4Range{
					Start: mustIP("192.168.20.20"),
					End:   mustIP("192.168.20.120"),
				},
			},
		},
		Exclusions: []model.Exclusion{
			{
				ID:     "ex-lan-printers",
				PoolID: "pool-lan",
				Range: model.IPv4Range{
					Start: mustIP("192.168.10.60"),
					End:   mustIP("192.168.10.69"),
				},
			},
			{
				ID:     "ex-lab-servers",
				PoolID: "pool-lab",
				Range: model.IPv4Range{
					Start: mustIP("192.168.20.30"),
					End:   mustIP("192.168.20.35"),
				},
			},
		},
		Reservations: []model.Reservation{
			{
				ID:        "res-lan-printer",
				PoolID:    "pool-lan",
				Hostname:  "lagerdrucker",
				MAC:       "52:54:00:12:34:56",
				IPAddress: mustIP("192.168.10.10"),
			},
			{
				ID:        "res-lab-nas",
				PoolID:    "pool-lab",
				Hostname:  "lab-nas",
				MAC:       "52:54:00:aa:bb:cc",
				IPAddress: mustIP("192.168.20.10"),
			},
		},
		Leases: []model.Lease{
			{
				ID:         "lease-lan-1",
				PoolID:     "pool-lan",
				Hostname:   "thinkpad-buero",
				MAC:        "00:11:22:33:44:55",
				IPAddress:  mustIP("192.168.10.81"),
				Duration:   12 * time.Hour,
				ExpiresAt:  time.Now().Add(7*time.Hour + 20*time.Minute),
				Vendor:     "Lenovo",
				ClientID:   "01:00:11:22:33:44:55",
				LastSeenAt: time.Now().Add(-3 * time.Minute),
			},
			{
				ID:         "lease-lan-2",
				PoolID:     "pool-lan",
				Hostname:   "scanner-lager",
				MAC:        "66:55:44:33:22:11",
				IPAddress:  mustIP("192.168.10.92"),
				Duration:   12 * time.Hour,
				ExpiresAt:  time.Now().Add(2*time.Hour + 5*time.Minute),
				Vendor:     "Brother",
				ClientID:   "scanner-92",
				LastSeenAt: time.Now().Add(-40 * time.Second),
			},
			{
				ID:         "lease-lab-1",
				PoolID:     "pool-lab",
				Hostname:   "ubuntu-testbox",
				MAC:        "de:ad:be:ef:20:01",
				IPAddress:  mustIP("192.168.20.80"),
				Duration:   8 * time.Hour,
				ExpiresAt:  time.Now().Add(6*time.Hour + 45*time.Minute),
				Vendor:     "Intel NIC",
				ClientID:   "lab-client-01",
				LastSeenAt: time.Now().Add(-90 * time.Second),
			},
		},
	}
}

func mustIP(raw string) net.IP {
	ip := net.ParseIP(raw)
	if ip == nil {
		panic("invalid IP in seed data: " + raw)
	}
	return ip
}

func mustCIDR(raw string) *net.IPNet {
	return validation.MustCIDR(raw)
}
