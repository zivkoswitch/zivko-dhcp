package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/parallels/dhcp-gui/internal/control"
	"github.com/parallels/dhcp-gui/internal/runtime"
	"github.com/parallels/dhcp-gui/internal/store"
	"github.com/parallels/dhcp-gui/internal/ui"
)

var version = "dev"

func main() {
	headless := flag.Bool("headless", false, "run DHCP server without GUI")
	guiOnly := flag.Bool("gui-only", false, "run GUI without starting an embedded DHCP server")
	configPath := flag.String("config-path", os.Getenv("DHCP_GUI_CONFIG_PATH"), "path to persistent config file")
	listenAddr := flag.String("listen-addr", os.Getenv("DHCP_GUI_LISTEN_ADDR"), "UDP listen address for the DHCP server")
	serverIP := flag.String("server-ip", os.Getenv("DHCP_GUI_SERVER_IP"), "server identifier IP for DHCP replies")
	interfaceName := flag.String("interface", os.Getenv("DHCP_GUI_INTERFACE"), "network interface to bind the DHCP server to")
	controlSocket := flag.String("control-socket", os.Getenv("DHCP_GUI_CONTROL_SOCKET"), "unix socket path for the local control API")
	flag.Parse()

	opts := runtime.Options{
		ConfigPath:    *configPath,
		ListenAddr:    *listenAddr,
		ServerIP:      *serverIP,
		InterfaceName: *interfaceName,
		ControlSocket: *controlSocket,
		Logger:        log.New(os.Stdout, "dhcpd: ", log.LstdFlags),
	}

	ctx, stop := runtime.SignalContext()
	defer stop()

	if *headless {
		opts.DefaultListen = ":67"
		opts.DefaultSocket = control.SystemSocketPath
		if err := runtime.RunHeadless(ctx, opts); err != nil {
			log.Fatal(err)
		}
		return
	}

	checkSocket := opts.ControlSocket
	if checkSocket == "" {
		defaultSocket, err := control.DefaultSocketPath()
		if err != nil {
			log.Fatal(err)
		}
		checkSocket = defaultSocket
	}

	startEmbedded := !*guiOnly && !runtime.DaemonAvailable(checkSocket)
	var embeddedStop context.CancelFunc
	embeddedOpts := opts
	if startEmbedded {
		opts.DefaultListen = ":6767"
		opts.DefaultSocket = checkSocket
		cancelEmbedded, err := runtime.StartEmbeddedServices(ctx, opts)
		if err != nil {
			log.Fatal(err)
		}
		embeddedStop = cancelEmbedded
		embeddedOpts = opts
	}

	repo, err := store.NewFileRepository(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	app := ui.NewApp(repo, ctx, embeddedOpts, embeddedStop)
	defer app.Shutdown()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func envOrDefault(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
