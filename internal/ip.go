package internal

import (
	"net"
	"os/exec"
	"strings"
)

func firstIPFromCommand(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	return firstIPFromOutput(string(out))
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
