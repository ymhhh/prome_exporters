//go:build !linux && !darwin && !windows

package internal

// GetIP todo ipv6
func GetIP() string {
	if ip := firstIPFromInterfaces(true /* preferIPv4 */); ip != "" {
		return ip
	}
	return firstIPFromInterfaces(false /* preferIPv4 */)
}
