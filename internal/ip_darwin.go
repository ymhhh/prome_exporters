//go:build darwin

package internal

// GetIP todo ipv6
func GetIP() string {
	// Most Macs use Wi‑Fi as en0; some use en1.
	if ip := firstIPFromCommand("ipconfig", "getifaddr", "en0"); ip != "" {
		return ip
	}
	if ip := firstIPFromCommand("ipconfig", "getifaddr", "en1"); ip != "" {
		return ip
	}

	if ip := firstIPFromInterfaces(true /* preferIPv4 */); ip != "" {
		return ip
	}
	return firstIPFromInterfaces(false /* preferIPv4 */)
}
