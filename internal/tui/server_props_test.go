package tui

import (
	"reflect"
	"testing"
)

func TestAffinityBits(t *testing.T) {
	cases := []struct {
		mask     uint32
		cpuCount int
		want     []bool
	}{
		{mask: 0, cpuCount: 4, want: []bool{false, false, false, false}},
		{mask: 1, cpuCount: 4, want: []bool{true, false, false, false}},
		{mask: 0b1010, cpuCount: 4, want: []bool{false, true, false, true}},
		{mask: affinityMask(-1), cpuCount: 3, want: []bool{true, true, true}}, // all bits set
	}
	for _, c := range cases {
		got := affinityBits(c.mask, c.cpuCount)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("affinityBits(%b, %d) = %v, want %v", c.mask, c.cpuCount, got, c.want)
		}
	}
}

func TestBitsToAffinity(t *testing.T) {
	cases := []struct {
		bits []bool
		want uint32
	}{
		{bits: []bool{false, false, false, false}, want: 0},
		{bits: []bool{true, false, false, false}, want: 1},
		{bits: []bool{false, true, false, true}, want: 0b1010},
	}
	for _, c := range cases {
		if got := bitsToAffinity(c.bits); got != c.want {
			t.Errorf("bitsToAffinity(%v) = %b, want %b", c.bits, got, c.want)
		}
	}
}

func TestAffinityBitsRoundTrip(t *testing.T) {
	for _, mask := range []uint32{0, 1, 0b1010, 0xFFFFFFFF} {
		bits := affinityBits(mask, 32)
		if got := bitsToAffinity(bits); got != mask {
			t.Errorf("round trip for mask %b: got %b", mask, got)
		}
	}
}

// TestAffinityValueStaysInSpConfiguresRange. 'affinity mask' is a 32-bit
// option, -2147483648…2147483647: CPU 31 is the sign bit, and the value
// written for it must be the stored value read back, not 2147483648 (Msg 8114).
func TestAffinityValueStaysInSpConfiguresRange(t *testing.T) {
	for _, stored := range []int64{0, 1, -2147483648, -1, 2147483647} {
		bits := affinityBits(affinityMask(stored), 32)
		if got := affinityValue(bitsToAffinity(bits)); got != stored {
			t.Errorf("stored %d round-trips to %d", stored, got)
		}
	}
	cpu31 := make([]bool, 32)
	cpu31[31] = true
	if got := affinityValue(bitsToAffinity(cpu31)); got != -2147483648 {
		t.Errorf("CPU 31 alone writes %d, want -2147483648", got)
	}
}
