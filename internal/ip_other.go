//go:build !linux && !darwin && !windows

package internal

func lookupIP() string {
	if ip := firstIPFromInterfaces(true /* preferIPv4 */); ip != "" {
		return ip
	}
	return firstIPFromInterfaces(false /* preferIPv4 */)
}
