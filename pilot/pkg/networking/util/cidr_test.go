package util

import (
	"testing"
)

func TestCIDRRangeOverlap(t *testing.T) {
	cases := map[string]struct {
		a      string
		b      string
		expect bool
	}{

		"all ips":                           {"0.0.0.0/0", "0.0.0.0/8", true},
		"subset":                            {"10.0.0.0/24", "10.0.0.0/16", true},
		"identical significant bits":        {"10.0.0.1/24", "10.0.0.2/24", true},
		"exclusive same subnet length":      {"10.1.0.0/24", "10.0.0.0/24", false},
		"exclusive different subnet length": {"11.0.0.0/24", "10.0.0.0/16", false},

		"all ips ipv6": {
			"0:0:0:0:0:0:0:0/0",
			"1234:1f1:123:123:f816:3eff:febf:57ce/32",
			true,
		},
		"identical significant bits ipv6": {
			"1234:1f1:123:123:f816:3eff:feb8:2287/32",
			"1234:1f1:123:123:f816:3eff:febf:57ce/32",
			true,
		},
		"subset ipv6": {
			"1:0:0:0:0:0:0:0/8",
			"1:2:0:0:0:0:0:0/16",
			true,
		},
		"exclusive same subnet length ipv6": {
			"1:0:0:0:0:0:0:0/32",
			"1:2:0:0:0:0:0:0/32",
			false,
		},
		"exclusive different subnet length ipv6": {
			"1:0:0:0:0:0:0:0/16",
			"2:0:0:0:0:0:0:0/32",
			false,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			a, b := ConvertAddressToCidr(tc.a), ConvertAddressToCidr(tc.b)
			if got := CIDRRangeOverlap(a, b); got != tc.expect {
				t.Errorf("expected %v for %s and %s but got %v", tc.expect, tc.a, tc.b, got)
			}
			// test that this is order-independent
			if got := CIDRRangeOverlap(b, a); got != tc.expect {
				t.Errorf("expected %v for %s and %s but got %v", tc.expect, tc.a, tc.b, got)
			}
		})
	}
}

var Res bool

func BenchmarkCIDRRangeOverlap(b *testing.B) {
	cases := map[string][]string{
		"overlapping": {"10.0.0.0/24", "10.0.0.0/28"},
		"exclusive":   {"10.0.0.0/24", "10.1.0.0/24"},
	}
	for name, cidrs := range cases {
		cidr, cidr2 := ConvertAddressToCidr(cidrs[0]), ConvertAddressToCidr(cidrs[1])
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				Res = CIDRRangeOverlap(cidr, cidr2)
			}
		})
	}
}
