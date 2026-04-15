package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/zivkotp/zivko-dhcp/internal/model"
	"github.com/zivkotp/zivko-dhcp/internal/store"
	"github.com/zivkotp/zivko-dhcp/internal/validation"
)

const SystemSocketPath = "/run/zivko-dhcp/zivko-dhcp.sock"

type Server struct {
	SocketPath string
	Repository store.Repository
	DHCPAddr   string
	ServerIP   string
}

func DefaultSocketPath() (string, error) {
	if info, err := os.Stat(filepath.Dir(SystemSocketPath)); err == nil && info.IsDir() {
		return SystemSocketPath, nil
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = os.TempDir()
	}
	return filepath.Join(runtimeDir, "zivko-dhcp.sock"), nil
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	socketPath := s.SocketPath
	if socketPath == "" {
		path, err := DefaultSocketPath()
		if err != nil {
			return err
		}
		socketPath = path
	}

	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		return err
	}
	_ = os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(socketPath)
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/v1/state", s.handleState)
	mux.HandleFunc("/v1/leases", s.handleLeases)

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	return server.Serve(listener)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":      "ok",
		"dhcp_addr":   s.DHCPAddr,
		"server_ip":   s.ServerIP,
		"socket_path": s.SocketPath,
	})
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, err := s.Repository.Load(context.Background())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	case http.MethodPut:
		var cfg model.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := validation.ValidateConfig(cfg); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.Repository.Save(context.Background(), cfg); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, cfg)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleLeases(w http.ResponseWriter, _ *http.Request) {
	cfg, err := s.Repository.Load(context.Background())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, cfg.Leases)
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

type Client struct {
	SocketPath string
}

type Health struct {
	Status     string `json:"status"`
	DHCPAddr   string `json:"dhcp_addr"`
	ServerIP   string `json:"server_ip"`
	SocketPath string `json:"socket_path"`
}

func (c *Client) Health(ctx context.Context) (Health, error) {
	var health Health
	err := c.get(ctx, "/healthz", &health)
	return health, err
}

func (c *Client) Leases(ctx context.Context) ([]model.Lease, error) {
	var leases []model.Lease
	if err := c.get(ctx, "/v1/leases", &leases); err != nil {
		return nil, err
	}
	return leases, nil
}

func (c *Client) State(ctx context.Context) (model.Config, error) {
	var cfg model.Config
	err := c.get(ctx, "/v1/state", &cfg)
	return cfg, err
}

func (c *Client) SaveState(ctx context.Context, cfg model.Config) error {
	return c.send(ctx, http.MethodPut, "/v1/state", cfg, &cfg)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	return c.send(ctx, http.MethodGet, path, nil, out)
}

func (c *Client) send(ctx context.Context, method, path string, payload any, out any) error {
	socketPath := c.SocketPath
	if socketPath == "" {
		var err error
		socketPath, err = DefaultSocketPath()
		if err != nil {
			return err
		}
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}

	var bodyReader io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, "http://unix"+path, bodyReader)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("control api returned %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
