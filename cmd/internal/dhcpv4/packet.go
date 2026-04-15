package dhcpv4

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

const (
	BootRequest = 1
	BootReply   = 2

	OptionSubnetMask           = 1
	OptionRouter               = 3
	OptionDNSServer            = 6
	OptionHostName             = 12
	OptionDomainName           = 15
	OptionRequestedIPAddress   = 50
	OptionIPAddressLeaseTime   = 51
	OptionMessageType          = 53
	OptionServerIdentifier     = 54
	OptionParameterRequestList = 55
	OptionRenewalTimeValue     = 58
	OptionRebindingTimeValue   = 59
	OptionVendorClassID        = 60
	OptionClientIdentifier     = 61
	OptionEnd                  = 255

	MessageDiscover = 1
	MessageOffer    = 2
	MessageRequest  = 3
	MessageDecline  = 4
	MessageAck      = 5
	MessageNak      = 6
	MessageRelease  = 7
	MessageInform   = 8
)

var magicCookie = []byte{99, 130, 83, 99}

type Packet struct {
	Op      byte
	HType   byte
	HLen    byte
	Hops    byte
	XID     uint32
	Secs    uint16
	Flags   uint16
	CIAddr  net.IP
	YIAddr  net.IP
	SIAddr  net.IP
	GIAddr  net.IP
	CHAddr  net.HardwareAddr
	Options map[byte][]byte
}

func ParsePacket(data []byte) (Packet, error) {
	if len(data) < 240 {
		return Packet{}, errors.New("packet too short")
	}
	if string(data[236:240]) != string(magicCookie) {
		return Packet{}, errors.New("missing dhcp magic cookie")
	}

	p := Packet{
		Op:      data[0],
		HType:   data[1],
		HLen:    data[2],
		Hops:    data[3],
		XID:     binary.BigEndian.Uint32(data[4:8]),
		Secs:    binary.BigEndian.Uint16(data[8:10]),
		Flags:   binary.BigEndian.Uint16(data[10:12]),
		CIAddr:  net.IP(data[12:16]).To4(),
		YIAddr:  net.IP(data[16:20]).To4(),
		SIAddr:  net.IP(data[20:24]).To4(),
		GIAddr:  net.IP(data[24:28]).To4(),
		CHAddr:  append(net.HardwareAddr(nil), data[28 : 28+16][:data[2]]...),
		Options: make(map[byte][]byte),
	}

	for i := 240; i < len(data); {
		code := data[i]
		i++
		if code == 0 {
			continue
		}
		if code == OptionEnd {
			break
		}
		if i >= len(data) {
			return Packet{}, errors.New("malformed dhcp option length")
		}
		length := int(data[i])
		i++
		if i+length > len(data) {
			return Packet{}, errors.New("malformed dhcp option value")
		}
		p.Options[code] = append([]byte(nil), data[i:i+length]...)
		i += length
	}

	return p, nil
}

func (p Packet) MarshalBinary() ([]byte, error) {
	buf := make([]byte, 240)
	buf[0] = p.Op
	buf[1] = p.HType
	buf[2] = p.HLen
	buf[3] = p.Hops
	binary.BigEndian.PutUint32(buf[4:8], p.XID)
	binary.BigEndian.PutUint16(buf[8:10], p.Secs)
	binary.BigEndian.PutUint16(buf[10:12], p.Flags)
	copy(buf[12:16], zeroOrIPv4(p.CIAddr))
	copy(buf[16:20], zeroOrIPv4(p.YIAddr))
	copy(buf[20:24], zeroOrIPv4(p.SIAddr))
	copy(buf[24:28], zeroOrIPv4(p.GIAddr))
	copy(buf[28:44], p.CHAddr)
	copy(buf[236:240], magicCookie)

	for code, value := range p.Options {
		if len(value) > 255 {
			return nil, fmt.Errorf("option %d too large", code)
		}
		buf = append(buf, code, byte(len(value)))
		buf = append(buf, value...)
	}
	buf = append(buf, OptionEnd)
	return buf, nil
}

func (p Packet) MessageType() byte {
	if value, ok := p.Options[OptionMessageType]; ok && len(value) > 0 {
		return value[0]
	}
	return 0
}

func (p Packet) RequestedIP() net.IP {
	if value, ok := p.Options[OptionRequestedIPAddress]; ok && len(value) == 4 {
		return net.IP(value).To4()
	}
	if p.CIAddr != nil && !p.CIAddr.Equal(net.IPv4zero) {
		return p.CIAddr.To4()
	}
	return nil
}

func (p Packet) ServerIdentifier() net.IP {
	if value, ok := p.Options[OptionServerIdentifier]; ok && len(value) == 4 {
		return net.IP(value).To4()
	}
	return nil
}

func (p Packet) Hostname() string {
	return string(p.Options[OptionHostName])
}

func (p Packet) ClientID() string {
	return string(p.Options[OptionClientIdentifier])
}

func (p Packet) VendorClassID() string {
	return string(p.Options[OptionVendorClassID])
}

func (p Packet) LeaseDuration() timeDuration {
	if value, ok := p.Options[OptionIPAddressLeaseTime]; ok && len(value) == 4 {
		seconds := binary.BigEndian.Uint32(value)
		return timeDuration(seconds)
	}
	return 0
}

func (p Packet) BroadcastRequested() bool {
	return p.Flags&0x8000 != 0
}

type timeDuration uint32

func NewReply(req Packet, messageType byte, serverIP, yourIP net.IP) Packet {
	options := map[byte][]byte{
		OptionMessageType:      {messageType},
		OptionServerIdentifier: zeroOrIPv4(serverIP),
	}
	return Packet{
		Op:      BootReply,
		HType:   req.HType,
		HLen:    req.HLen,
		XID:     req.XID,
		Flags:   req.Flags,
		CIAddr:  req.CIAddr,
		YIAddr:  yourIP,
		SIAddr:  serverIP,
		GIAddr:  req.GIAddr,
		CHAddr:  req.CHAddr,
		Options: options,
	}
}

func zeroOrIPv4(ip net.IP) []byte {
	if ip == nil {
		return []byte{0, 0, 0, 0}
	}
	if v4 := ip.To4(); v4 != nil {
		return v4
	}
	return []byte{0, 0, 0, 0}
}
