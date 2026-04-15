package model

import (
	"net"
	"time"
)

type IPv4Range struct {
	Start net.IP
	End   net.IP
}

type Pool struct {
	ID             string
	Name           string
	Subnet         *net.IPNet
	Range          IPv4Range
	DefaultGateway net.IP
	DNSServers     []net.IP
	DomainName     string
}

type Exclusion struct {
	ID     string
	PoolID string
	Range  IPv4Range
}

type Reservation struct {
	ID        string
	PoolID    string
	Hostname  string
	MAC       string
	IPAddress net.IP
}

type Lease struct {
	ID         string
	PoolID     string
	Hostname   string
	MAC        string
	IPAddress  net.IP
	ExpiresAt  time.Time
	Duration   time.Duration
	Vendor     string
	ClientID   string
	LastSeenAt time.Time
}

type RuntimeSettings struct {
	ListenAddr    string
	ServerIP      string
	InterfaceName string
	ControlSocket string
}

type Config struct {
	Runtime      RuntimeSettings
	Pools        []Pool
	Exclusions   []Exclusion
	Reservations []Reservation
	Leases       []Lease
}
