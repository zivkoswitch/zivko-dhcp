package dhcpv4

import (
	"net"
	"testing"
)

func TestPacketMarshalRoundTrip(t *testing.T) {
	t.Parallel()

	original := Packet{
		Op:     BootRequest,
		HType:  1,
		HLen:   6,
		XID:    1234,
		CHAddr: net.HardwareAddr{1, 2, 3, 4, 5, 6},
		Options: map[byte][]byte{
			OptionMessageType:        {MessageDiscover},
			OptionRequestedIPAddress: net.IPv4(192, 168, 1, 50).To4(),
			OptionHostName:           []byte("client-a"),
		},
	}

	data, err := original.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	parsed, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("ParsePacket() error = %v", err)
	}
	if parsed.MessageType() != MessageDiscover {
		t.Fatalf("message type = %d, want %d", parsed.MessageType(), MessageDiscover)
	}
	if got := parsed.RequestedIP().String(); got != "192.168.1.50" {
		t.Fatalf("requested ip = %s", got)
	}
	if got := parsed.Hostname(); got != "client-a" {
		t.Fatalf("hostname = %q", got)
	}
}

func TestPacketParsesServerIdentifierAndBroadcastFlag(t *testing.T) {
	t.Parallel()

	packet := Packet{
		Op:     BootRequest,
		HType:  1,
		HLen:   6,
		Flags:  0x8000,
		XID:    42,
		CHAddr: net.HardwareAddr{1, 2, 3, 4, 5, 6},
		Options: map[byte][]byte{
			OptionMessageType:      {MessageRequest},
			OptionServerIdentifier: net.IPv4(10, 0, 0, 1).To4(),
		},
	}

	data, err := packet.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary() error = %v", err)
	}
	parsed, err := ParsePacket(data)
	if err != nil {
		t.Fatalf("ParsePacket() error = %v", err)
	}
	if !parsed.BroadcastRequested() {
		t.Fatal("expected broadcast flag")
	}
	if got := parsed.ServerIdentifier().String(); got != "10.0.0.1" {
		t.Fatalf("server identifier = %s", got)
	}
}
