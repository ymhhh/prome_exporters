package internal

import (
	"net"
	"testing"
)

func Test_firstIPFromOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "spaces", in: "   \n\t", want: ""},
		{name: "single_ipv4", in: "192.168.1.10\n", want: "192.168.1.10"},
		{name: "single_ipv6", in: "2001:db8::1\n", want: "2001:db8::1"},
		{name: "multiple_tokens_pick_first", in: "192.168.1.10 10.0.0.2\n", want: "192.168.1.10"},
		{name: "first_token_invalid", in: "not-an-ip 192.168.1.10\n", want: ""},
		{name: "loopback_ipv4", in: "127.0.0.1\n", want: ""},
		{name: "linklocal_ipv4", in: "169.254.1.2\n", want: ""},
		{name: "linklocal_ipv6", in: "fe80::1\n", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstIPFromOutput(tt.in); got != tt.want {
				t.Fatalf("firstIPFromOutput(%q)=%q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func Test_candidatesFromAddrs_and_chooseCandidate(t *testing.T) {
	t.Parallel()

	addrs := []net.Addr{
		&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
		&net.IPNet{IP: net.ParseIP("169.254.1.2"), Mask: net.CIDRMask(16, 32)},
		&net.IPNet{IP: net.ParseIP("192.168.1.10"), Mask: net.CIDRMask(24, 32)},
		&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)},
		&net.IPNet{IP: net.ParseIP("2001:db8::1"), Mask: net.CIDRMask(64, 128)},
	}

	v4, v6 := candidatesFromAddrs(addrs)
	if v4 != "192.168.1.10" {
		t.Fatalf("v4Candidate=%q, want %q", v4, "192.168.1.10")
	}
	if v6 != "2001:db8::1" {
		t.Fatalf("v6Candidate=%q, want %q", v6, "2001:db8::1")
	}

	if got := chooseCandidate(true, v4, v6); got != "192.168.1.10" {
		t.Fatalf("chooseCandidate(preferIPv4=true)=%q, want %q", got, "192.168.1.10")
	}
	if got := chooseCandidate(false, v4, v6); got != "2001:db8::1" {
		t.Fatalf("chooseCandidate(preferIPv4=false)=%q, want %q", got, "2001:db8::1")
	}
}
