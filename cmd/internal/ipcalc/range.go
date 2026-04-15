package ipcalc

import (
	"encoding/binary"
	"fmt"
	"net"
	"sort"

	"github.com/zivkotp/zivko-dhcp/internal/model"
)

func NormalizeIPv4(ip net.IP) (net.IP, error) {
	if ip == nil {
		return nil, fmt.Errorf("ip is required")
	}
	v4 := ip.To4()
	if v4 == nil {
		return nil, fmt.Errorf("only IPv4 is supported")
	}
	return v4, nil
}

func NormalizeRange(r model.IPv4Range) (model.IPv4Range, error) {
	start, err := NormalizeIPv4(r.Start)
	if err != nil {
		return model.IPv4Range{}, fmt.Errorf("invalid start ip: %w", err)
	}
	end, err := NormalizeIPv4(r.End)
	if err != nil {
		return model.IPv4Range{}, fmt.Errorf("invalid end ip: %w", err)
	}
	if CompareIP(start, end) > 0 {
		return model.IPv4Range{}, fmt.Errorf("range start must be before or equal to end")
	}
	return model.IPv4Range{Start: start, End: end}, nil
}

func CompareIP(a, b net.IP) int {
	aValue := IPToUint32(a)
	bValue := IPToUint32(b)
	switch {
	case aValue < bValue:
		return -1
	case aValue > bValue:
		return 1
	default:
		return 0
	}
}

func IPToUint32(ip net.IP) uint32 {
	v4 := ip.To4()
	return binary.BigEndian.Uint32(v4)
}

func Uint32ToIP(value uint32) net.IP {
	out := make(net.IP, net.IPv4len)
	binary.BigEndian.PutUint32(out, value)
	return out
}

func ContainsIP(r model.IPv4Range, ip net.IP) bool {
	return CompareIP(ip, r.Start) >= 0 && CompareIP(ip, r.End) <= 0
}

func ContainsRange(parent, child model.IPv4Range) bool {
	return CompareIP(child.Start, parent.Start) >= 0 && CompareIP(child.End, parent.End) <= 0
}

func Overlaps(a, b model.IPv4Range) bool {
	return CompareIP(a.Start, b.End) <= 0 && CompareIP(b.Start, a.End) <= 0
}

func SortRanges(ranges []model.IPv4Range) {
	sort.Slice(ranges, func(i, j int) bool {
		if CompareIP(ranges[i].Start, ranges[j].Start) == 0 {
			return CompareIP(ranges[i].End, ranges[j].End) < 0
		}
		return CompareIP(ranges[i].Start, ranges[j].Start) < 0
	})
}

func EffectiveRanges(pool model.IPv4Range, exclusions []model.IPv4Range) ([]model.IPv4Range, error) {
	normalizedPool, err := NormalizeRange(pool)
	if err != nil {
		return nil, err
	}

	normExclusions := make([]model.IPv4Range, 0, len(exclusions))
	for _, exclusion := range exclusions {
		norm, err := NormalizeRange(exclusion)
		if err != nil {
			return nil, err
		}
		if !ContainsRange(normalizedPool, norm) {
			return nil, fmt.Errorf("exclusion %s-%s is outside pool", norm.Start, norm.End)
		}
		normExclusions = append(normExclusions, norm)
	}
	SortRanges(normExclusions)

	currentStart := IPToUint32(normalizedPool.Start)
	poolEnd := IPToUint32(normalizedPool.End)
	result := make([]model.IPv4Range, 0, len(normExclusions)+1)

	for _, exclusion := range normExclusions {
		exStart := IPToUint32(exclusion.Start)
		exEnd := IPToUint32(exclusion.End)

		if exStart > currentStart {
			result = append(result, model.IPv4Range{
				Start: Uint32ToIP(currentStart),
				End:   Uint32ToIP(exStart - 1),
			})
		}

		if exEnd == ^uint32(0) {
			currentStart = exEnd
			continue
		}
		if exEnd+1 > currentStart {
			currentStart = exEnd + 1
		}
	}

	if currentStart <= poolEnd {
		result = append(result, model.IPv4Range{
			Start: Uint32ToIP(currentStart),
			End:   Uint32ToIP(poolEnd),
		})
	}

	return result, nil
}
