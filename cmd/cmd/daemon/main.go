package main

import (
	"log"
	"os"

	"github.com/parallels/dhcp-gui/internal/control"
	"github.com/parallels/dhcp-gui/internal/runtime"
)

func main() {
	ctx, stop := runtime.SignalContext()
	defer stop()

	if err := runtime.RunHeadless(ctx, runtime.Options{
		ConfigPath:    envOrDefault("DHCP_GUI_CONFIG_PATH", ""),
		ListenAddr:    os.Getenv("DHCP_GUI_LISTEN_ADDR"),
		DefaultListen: ":67",
		ServerIP:      os.Getenv("DHCP_GUI_SERVER_IP"),
		InterfaceName: envOrDefault("DHCP_GUI_INTERFACE", ""),
		ControlSocket: envOrDefault("DHCP_GUI_CONTROL_SOCKET", control.SystemSocketPath),
		DefaultSocket: control.SystemSocketPath,
		Logger:        log.New(os.Stdout, "dhcpd: ", log.LstdFlags),
	}); err != nil {
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
