//go:build linux

package internal

// GetIP todo ipv6
func GetIP() string {
	if ip := firstIPFromCommand("hostname", "-I"); ip != "" {
		return ip
	}
	if ip := firstIPFromCommand("hostname", "-i"); ip != "" {
		return ip
	}

	if ip := firstIPFromInterfaces(true /* preferIPv4 */); ip != "" {
		return ip
	}
	return firstIPFromInterfaces(false /* preferIPv4 */)
}
