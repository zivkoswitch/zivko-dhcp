package main

import (
	"log"
	"os"

	"github.com/zivkotp/zivko-dhcp/internal/control"
	"github.com/zivkotp/zivko-dhcp/internal/runtime"
)

func main() {
	ctx, stop := runtime.SignalContext()
	defer stop()

	if err := runtime.RunHeadless(ctx, runtime.Options{
		ConfigPath:    envOrDefault("ZIVKO_DHCP_CONFIG_PATH", ""),
		ListenAddr:    os.Getenv("ZIVKO_DHCP_LISTEN_ADDR"),
		DefaultListen: ":67",
		ServerIP:      os.Getenv("ZIVKO_DHCP_SERVER_IP"),
		InterfaceName: envOrDefault("ZIVKO_DHCP_INTERFACE", ""),
		ControlSocket: envOrDefault("ZIVKO_DHCP_CONTROL_SOCKET", control.SystemSocketPath),
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
