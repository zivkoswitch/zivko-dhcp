package runtime

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/parallels/dhcp-gui/internal/control"
	"github.com/parallels/dhcp-gui/internal/dhcpv4"
	"github.com/parallels/dhcp-gui/internal/model"
	"github.com/parallels/dhcp-gui/internal/store"
)

const DefaultListenPort = "6767"

type Options struct {
	ConfigPath    string
	ListenAddr    string
	DefaultListen string
	ServerIP      string
	InterfaceName string
	ControlSocket string
	DefaultSocket string
	Logger        *log.Logger
}

type InterfaceInfo struct {
	Name     string
	IPv4     string
	Up       bool
	Loopback bool
}

func RunHeadless(ctx context.Context, opts Options) error {
	repo, err := store.NewFileRepository(opts.ConfigPath)
	if err != nil {
		return err
	}
	cfg, err := repo.Load(context.Background())
	if err != nil {
		return err
	}
	opts = applyRuntimeConfig(opts, cfg.Runtime)
	serverIP := resolvedServerIP(opts)

	logger := opts.Logger
	if logger == nil {
		logger = log.New(os.Stdout, "dhcpd: ", log.LstdFlags)
	}

	server := &dhcpv4.Server{
		Addr:          opts.ListenAddr,
		ServerIP:      net.ParseIP(serverIP),
		InterfaceName: opts.InterfaceName,
		Repository:    repo,
		Logger:        logger,
	}
	controlServer := &control.Server{
		SocketPath: opts.ControlSocket,
		Repository: repo,
		DHCPAddr:   opts.ListenAddr,
		ServerIP:   serverIP,
	}

	errCh := make(chan error, 2)

	go func() {
		logger.Printf("starting control socket server")
		if err := controlServer.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
			errCh <- err
		}
	}()

	go func() {
		logger.Printf("starting Go DHCP server on %s", server.Addr)
		if err := server.ListenAndServe(ctx); err != nil && ctx.Err() == nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		return err
	}
}

func StartEmbeddedServices(parent context.Context, opts Options) (context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(parent)
	go func() {
		if err := RunHeadless(ctx, opts); err != nil && ctx.Err() == nil {
			log.Printf("embedded dhcp runtime stopped: %v", err)
			cancel()
		}
	}()

	deadline := time.Now().Add(2 * time.Second)
	client := &control.Client{SocketPath: opts.ControlSocket}
	for time.Now().Before(deadline) {
		syncCtx, syncCancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		_, err := client.Health(syncCtx)
		syncCancel()
		if err == nil {
			return cancel, nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	cancel()
	return nil, fmt.Errorf("embedded daemon did not become ready")
}

func DaemonAvailable(socketPath string) bool {
	client := &control.Client{SocketPath: socketPath}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := client.Health(ctx)
	return err == nil
}

func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func applyRuntimeConfig(opts Options, runtimeCfg model.RuntimeSettings) Options {
	if opts.ListenAddr == "" {
		opts.ListenAddr = runtimeCfg.ListenAddr
	}
	if opts.ServerIP == "" {
		opts.ServerIP = runtimeCfg.ServerIP
	}
	if opts.InterfaceName == "" {
		opts.InterfaceName = runtimeCfg.InterfaceName
	}
	if opts.ControlSocket == "" {
		opts.ControlSocket = runtimeCfg.ControlSocket
	}
	if opts.InterfaceName == "" {
		opts.InterfaceName = PreferredInterfaceName()
	}
	if opts.ControlSocket == "" {
		if opts.DefaultSocket != "" {
			opts.ControlSocket = opts.DefaultSocket
		} else {
			socketPath, err := control.DefaultSocketPath()
			if err == nil {
				opts.ControlSocket = socketPath
			}
		}
	}
	if opts.ListenAddr == "" {
		if opts.DefaultListen != "" {
			opts.ListenAddr = opts.DefaultListen
		} else {
			opts.ListenAddr = NormalizeListenAddr("")
		}
	}
	opts.ListenAddr = NormalizeListenAddr(opts.ListenAddr)
	return opts
}

func DetectInterfaces() ([]InterfaceInfo, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}

	out := make([]InterfaceInfo, 0, len(ifaces))
	for _, iface := range ifaces {
		info := InterfaceInfo{
			Name:     iface.Name,
			Up:       iface.Flags&net.FlagUp != 0,
			Loopback: iface.Flags&net.FlagLoopback != 0,
		}

		addrs, err := iface.Addrs()
		if err == nil {
			for _, addr := range addrs {
				ip := ipFromAddr(addr)
				if ip == nil {
					continue
				}
				info.IPv4 = ip.String()
				break
			}
		}

		out = append(out, info)
	}

	sort.Slice(out, func(i, j int) bool {
		leftScore := interfaceScore(out[i])
		rightScore := interfaceScore(out[j])
		if leftScore != rightScore {
			return leftScore > rightScore
		}
		return out[i].Name < out[j].Name
	})

	return out, nil
}

func PreferredInterfaceName() string {
	interfaces, err := DetectInterfaces()
	if err != nil {
		return ""
	}
	for _, iface := range interfaces {
		if iface.IPv4 != "" {
			return iface.Name
		}
	}
	if len(interfaces) == 0 {
		return ""
	}
	return interfaces[0].Name
}

func DetectInterfaceIPv4(name string) string {
	if name == "" {
		name = PreferredInterfaceName()
	}
	if name == "" {
		return ""
	}

	iface, err := net.InterfaceByName(name)
	if err != nil {
		return ""
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		ip := ipFromAddr(addr)
		if ip != nil {
			return ip.String()
		}
	}
	return ""
}

func NormalizeListenAddr(raw string) string {
	port := ListenPort(raw)
	return ":" + port
}

func ListenPort(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultListenPort
	}
	if strings.HasPrefix(raw, ":") {
		raw = strings.TrimPrefix(raw, ":")
	}
	if host, port, err := net.SplitHostPort(raw); err == nil {
		if port != "" {
			return port
		}
		if host == "" {
			return DefaultListenPort
		}
	}
	return raw
}

func resolvedServerIP(opts Options) string {
	if opts.ServerIP != "" {
		return opts.ServerIP
	}
	if ip := DetectInterfaceIPv4(opts.InterfaceName); ip != "" {
		return ip
	}
	return "0.0.0.0"
}

func ipFromAddr(addr net.Addr) net.IP {
	switch value := addr.(type) {
	case *net.IPNet:
		if ip := value.IP.To4(); ip != nil {
			return ip
		}
	case *net.IPAddr:
		if ip := value.IP.To4(); ip != nil {
			return ip
		}
	}
	return nil
}

func interfaceScore(info InterfaceInfo) int {
	score := 0
	if info.Up {
		score += 4
	}
	if !info.Loopback {
		score += 2
	}
	if info.IPv4 != "" {
		score++
	}
	return score
}
