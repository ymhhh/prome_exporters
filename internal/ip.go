package internal

import (
	"net"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

type ipCache struct {
	mu sync.Mutex
	v  atomic.Value // string
}

func (c *ipCache) get(lookup func() string) string {
	if v, ok := c.v.Load().(string); ok && v != "" {
		return v
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.v.Load().(string); ok && v != "" {
		return v
	}
	ip := lookup()
	if ip != "" {
		c.v.Store(ip)
	}
	return ip
}

var defaultIPCache ipCache

// GetIP returns a cached host IP. Empty lookups are not cached so a later
// scrape can still succeed if the interface was not ready at startup.
func GetIP() string {
	return defaultIPCache.get(lookupIP)
}

func firstIPFromCommand(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return firstIPFromOutput(string(out))
}

// netDial is swapped in tests to inject a fake UDP connection.
var netDial = net.Dial

// firstIPFromDefaultRoute returns the source IP the kernel would use for the
// default route. UDP "connect" only consults the routing table; it does not
// send packets. This is more accurate than `hostname -I`, whose address order
// is unstable and often includes docker/VPN/bridge IPs.
func firstIPFromDefaultRoute() string {
	if ip := sourceIPFor("udp4", "192.0.2.1:80"); ip != "" {
		return ip
	}
	return sourceIPFor("udp6", "[2001:db8::1]:80")
}

func sourceIPFor(network, address string) string {
	conn, err := netDial(network, address)
	if err != nil {
		return ""
	}
	defer conn.Close()
	udp, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || udp == nil || udp.IP == nil {
		return ""
	}
	return sanitizeIP(udp.IP.String())
}

// firstIPFromOutput parses output that may contain multiple IPs and returns
// the first valid global-unicast IP (preferring IPv4 if present as the first token).
func firstIPFromOutput(out string) string {
	// Commands may return multiple addresses; pick the first token.
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) == 0 {
		return ""
	}
	return sanitizeIP(fields[0])
}

func sanitizeIP(s string) string {
	ip := net.ParseIP(s)
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsLoopback() {
		return ""
	}
	if ip4 := ip.To4(); ip4 != nil {
		if isIPv4LinkLocal(ip4) {
			return ""
		}
		return ip4.String()
	}
	// IPv6
	if ip.IsLinkLocalUnicast() {
		return ""
	}
	return ip.String()
}

func firstIPFromInterfaces(preferIPv4 bool) string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	for _, iface := range ifaces {
		if (iface.Flags&net.FlagUp) == 0 || (iface.Flags&net.FlagLoopback) != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		v4Candidate, v6Candidate := candidatesFromAddrs(addrs)
		if chosen := chooseCandidate(preferIPv4, v4Candidate, v6Candidate); chosen != "" {
			return chosen
		}
	}
	return ""
}

func candidatesFromAddrs(addrs []net.Addr) (v4Candidate string, v6Candidate string) {
	for _, addr := range addrs {
		ipnet, ok := addr.(*net.IPNet)
		if !ok || ipnet.IP == nil {
			continue
		}
		ip := ipnet.IP
		if !ip.IsGlobalUnicast() || ip.IsLoopback() {
			continue
		}

		if ip4 := ip.To4(); ip4 != nil {
			if isIPv4LinkLocal(ip4) {
				continue
			}
			if v4Candidate == "" {
				v4Candidate = ip4.String()
			}
			continue
		}

		// IPv6
		if ip.IsLinkLocalUnicast() {
			continue
		}
		if v6Candidate == "" {
			v6Candidate = ip.String()
		}
	}
	return v4Candidate, v6Candidate
}

func chooseCandidate(preferIPv4 bool, v4Candidate string, v6Candidate string) string {
	if preferIPv4 {
		if v4Candidate != "" {
			return v4Candidate
		}
		return v6Candidate
	}
	if v6Candidate != "" {
		return v6Candidate
	}
	return v4Candidate
}

func isIPv4LinkLocal(ip net.IP) bool {
	// 169.254.0.0/16
	return len(ip) == net.IPv4len && ip[0] == 169 && ip[1] == 254
}
