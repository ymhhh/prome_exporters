package internal

import (
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
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

func TestIPCacheStoresFirstSuccess(t *testing.T) {
	var c ipCache
	var n atomic.Int32
	lookup := func() string {
		n.Add(1)
		return "10.0.0.1"
	}
	if got := c.get(lookup); got != "10.0.0.1" {
		t.Fatalf("first get=%q", got)
	}
	if got := c.get(lookup); got != "10.0.0.1" {
		t.Fatalf("second get=%q", got)
	}
	if n.Load() != 1 {
		t.Fatalf("lookup called %d times, want 1", n.Load())
	}
}

func TestIPCacheRetriesEmpty(t *testing.T) {
	var c ipCache
	var n atomic.Int32
	lookup := func() string {
		if n.Add(1) == 1 {
			return ""
		}
		return "10.0.0.1"
	}
	if got := c.get(lookup); got != "" {
		t.Fatalf("first get=%q, want empty", got)
	}
	if got := c.get(lookup); got != "10.0.0.1" {
		t.Fatalf("second get=%q, want 10.0.0.1", got)
	}
	if n.Load() != 2 {
		t.Fatalf("lookup called %d times, want 2", n.Load())
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

type stubUDPConn struct {
	local net.Addr
}

func (s stubUDPConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (s stubUDPConn) Write(b []byte) (int, error)      { return len(b), nil }
func (s stubUDPConn) Close() error                     { return nil }
func (s stubUDPConn) LocalAddr() net.Addr              { return s.local }
func (s stubUDPConn) RemoteAddr() net.Addr             { return s.local }
func (s stubUDPConn) SetDeadline(time.Time) error      { return nil }
func (s stubUDPConn) SetReadDeadline(time.Time) error  { return nil }
func (s stubUDPConn) SetWriteDeadline(time.Time) error { return nil }

func TestSourceIPForUsesLocalUDPAddr(t *testing.T) {
	orig := netDial
	t.Cleanup(func() { netDial = orig })
	netDial = func(network, address string) (net.Conn, error) {
		return stubUDPConn{local: &net.UDPAddr{IP: net.ParseIP("10.1.2.3"), Port: 12345}}, nil
	}
	if got := sourceIPFor("udp4", "192.0.2.1:80"); got != "10.1.2.3" {
		t.Fatalf("sourceIPFor=%q, want 10.1.2.3", got)
	}
}

func TestSourceIPForRejectsLoopback(t *testing.T) {
	orig := netDial
	t.Cleanup(func() { netDial = orig })
	netDial = func(network, address string) (net.Conn, error) {
		return stubUDPConn{local: &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)}}, nil
	}
	if got := sourceIPFor("udp4", "192.0.2.1:80"); got != "" {
		t.Fatalf("sourceIPFor=%q, want empty", got)
	}
}

func TestFirstIPFromDefaultRoutePrefersIPv4(t *testing.T) {
	orig := netDial
	t.Cleanup(func() { netDial = orig })
	netDial = func(network, address string) (net.Conn, error) {
		if network == "udp4" {
			return stubUDPConn{local: &net.UDPAddr{IP: net.ParseIP("192.168.0.10")}}, nil
		}
		return stubUDPConn{local: &net.UDPAddr{IP: net.ParseIP("2001:db8::1")}}, nil
	}
	if got := firstIPFromDefaultRoute(); got != "192.168.0.10" {
		t.Fatalf("firstIPFromDefaultRoute=%q, want 192.168.0.10", got)
	}
}
