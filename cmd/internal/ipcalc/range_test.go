package ipcalc

import (
	"net"
	"testing"

	"github.com/zivkotp/zivko-dhcp/internal/model"
)

func mustIP(t *testing.T, raw string) net.IP {
	t.Helper()
	ip := net.ParseIP(raw)
	if ip == nil {
		t.Fatalf("failed to parse ip %q", raw)
	}
	return ip
}

func TestEffectiveRangesSubtractsExclusionsDeterministically(t *testing.T) {
	t.Parallel()

	pool := model.IPv4Range{
		Start: mustIP(t, "192.168.10.10"),
		End:   mustIP(t, "192.168.10.20"),
	}
	exclusions := []model.IPv4Range{
		{Start: mustIP(t, "192.168.10.15"), End: mustIP(t, "192.168.10.16")},
		{Start: mustIP(t, "192.168.10.12"), End: mustIP(t, "192.168.10.13")},
	}

	got, err := EffectiveRanges(pool, exclusions)
	if err != nil {
		t.Fatalf("EffectiveRanges() error = %v", err)
	}

	want := []model.IPv4Range{
		{Start: mustIP(t, "192.168.10.10"), End: mustIP(t, "192.168.10.11")},
		{Start: mustIP(t, "192.168.10.14"), End: mustIP(t, "192.168.10.14")},
		{Start: mustIP(t, "192.168.10.17"), End: mustIP(t, "192.168.10.20")},
	}

	if len(got) != len(want) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(want))
	}

	for i := range want {
		if !got[i].Start.Equal(want[i].Start) || !got[i].End.Equal(want[i].End) {
			t.Fatalf("range[%d] = %s-%s, want %s-%s", i, got[i].Start, got[i].End, want[i].Start, want[i].End)
		}
	}
}

func TestEffectiveRangesRejectsOutOfBoundsExclusion(t *testing.T) {
	t.Parallel()

	pool := model.IPv4Range{
		Start: mustIP(t, "10.0.0.10"),
		End:   mustIP(t, "10.0.0.20"),
	}
	exclusions := []model.IPv4Range{
		{Start: mustIP(t, "10.0.0.9"), End: mustIP(t, "10.0.0.11")},
	}

	if _, err := EffectiveRanges(pool, exclusions); err == nil {
		t.Fatal("expected error for exclusion outside pool")
	}
}
